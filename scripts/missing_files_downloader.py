#!/usr/bin/env python3
"""
PyPI Missing Files Downloader
Function:
1. Scan input data directory for subdirectories (package names).
2. Read 'missing_files' in each subdirectory.
3. Filter files based on platform (Source, Any, Linux/Windows x86/AMD64; Exclude MacOS, ARM, etc).
4. Download filtered files to the path specified in 'missing_files' (relative to package dir).
5. Support resume (Range header).
"""

import os
import sys
import argparse
import logging
import threading
import queue
import time
import requests
import signal
import json
import errno
import random
from pathlib import Path
from urllib.parse import urljoin, unquote, urlparse
from typing import Optional, Tuple

import concurrent.futures
from html.parser import HTMLParser

# Configure logging
# Remove StreamHandler to avoid cluttering stdout
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler('downloader.log')
    ]
)
logger = logging.getLogger(__name__)

import subprocess

# Random User-Agent list to avoid rate limiting
USER_AGENTS = [
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
    'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0',
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:121.0) Gecko/20100101 Firefox/121.0',
    'pip/23.3.1',
    'pip/24.0',
    'curl/8.4.0',
    'Wget/1.21.4'
]

class ExternalDownloader:
    @staticmethod
    def download_with_wget(url: str, output_path: Path, user_agent: str, timeout: int = 60) -> bool:
        cmd = [
            'wget',
            '-q', # Quiet
            '--user-agent', user_agent,
            '--timeout', str(timeout),
            '--tries', '3',
            '-O', str(output_path),
            url
        ]
        try:
            subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            return True
        except subprocess.CalledProcessError:
            return False

    @staticmethod
    def download_with_curl(url: str, output_path: Path, user_agent: str, timeout: int = 60) -> bool:
        cmd = [
            'curl',
            '-s', # Silent
            '-L', # Follow redirects
            '-A', user_agent,
            '--connect-timeout', '10',
            '--max-time', str(timeout),
            '--retry', '3',
            '-o', str(output_path),
            url
        ]
        try:
            subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            return True
        except subprocess.CalledProcessError:
            return False

    @staticmethod
    def download_with_aria2(url: str, output_path: Path, user_agent: str, timeout: int = 60) -> bool:
        cmd = [
            'aria2c',
            '-q', # Quiet
            '--user-agent', user_agent,
            '--connect-timeout', '10',
            '--timeout', str(timeout),
            '--max-tries', '3',
            '-d', str(output_path.parent),
            '-o', output_path.name,
            '--allow-overwrite=true',
            '--auto-file-renaming=false', # Prevent renaming
            url
        ]
        try:
            subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            return True
        except subprocess.CalledProcessError:
            return False

try:
    import psutil
    PSUTIL_AVAILABLE = True
except ImportError:
    PSUTIL_AVAILABLE = False

class StatusPrinter(threading.Thread):
    def __init__(self, downloader, interval=1.0):
        super().__init__()
        self.downloader = downloader
        self.interval = interval
        self.stop_event = threading.Event()
        self.daemon = True
        self.last_bytes = 0
        self.last_time = time.time()
        self.last_sys_bytes = self.get_system_bytes()

    def get_system_bytes(self):
        """Get total system receive bytes"""
        if PSUTIL_AVAILABLE:
            return psutil.net_io_counters().bytes_recv
        
        # Fallback for Linux without psutil
        try:
            with open('/proc/net/dev', 'r') as f:
                lines = f.readlines()
            total_bytes = 0
            for line in lines[2:]:
                if ':' in line:
                    iface, data = line.split(':', 1)
                    if iface.strip() == 'lo':
                        continue
                    total_bytes += int(data.split()[0])
            return total_bytes
        except:
            return 0

    def run(self):
        while not self.stop_event.is_set():
            self.print_status()
            time.sleep(self.interval)
        self.print_status()
        print() # New line at the end

    def format_speed(self, speed):
        if speed < 1024:
            return f"{speed:.1f} B/s"
        elif speed < 1024 * 1024:
            return f"{speed/1024:.1f} KB/s"
        else:
            return f"{speed/(1024*1024):.1f} MB/s"

    def print_status(self):
        current_time = time.time()
        current_bytes = self.downloader.downloaded_bytes
        current_sys_bytes = self.get_system_bytes()
        
        # Calculate speeds
        time_diff = current_time - self.last_time
        bytes_diff = current_bytes - self.last_bytes
        sys_bytes_diff = current_sys_bytes - self.last_sys_bytes
        
        total_speed = 0
        sys_speed = 0
        if time_diff > 0:
            total_speed = bytes_diff / time_diff
            sys_speed = sys_bytes_diff / time_diff
            
        self.last_time = current_time
        self.last_bytes = current_bytes
        self.last_sys_bytes = current_sys_bytes
        
        # Get thread speeds
        thread_speeds = []
        with self.downloader.lock:
            processed = self.downloader.processed_tasks
            total = self.downloader.total_tasks
            success = self.downloader.success_count
            fail = self.downloader.fail_count
            not_found = self.downloader.not_found_count
            q_size = self.downloader.queue.qsize()
            
            # Calculate instantaneous speed for each thread
            for i, tracker in enumerate(self.downloader.thread_trackers):
                t_bytes = tracker['bytes']
                t_last_bytes = tracker['last_bytes']
                t_diff = t_bytes - t_last_bytes
                t_speed = 0
                if time_diff > 0:
                    t_speed = t_diff / time_diff
                tracker['last_bytes'] = t_bytes # Update for next tick
                thread_speeds.append(f"T{i+1}:{self.format_speed(t_speed)}")

        percent = 0.0
        if total > 0:
            percent = (processed / total) * 100
            
        # Build status lines
        # Line 1: Overall stats
        line1 = (
            f"Progress: {processed}/{total} ({percent:.1f}%) | "
            f"Success: {success} | Failed: {fail} | 404: {not_found} | "
            f"App Speed: {self.format_speed(total_speed)} | Sys Speed: {self.format_speed(sys_speed)} | Queue: {q_size}"
        )
        
        # Line 2: Thread speeds
        line2 = " | ".join(thread_speeds)
        
        # Clear screen/lines? No, just carriage return and overwrite.
        # But we have 2 lines now.
        # Use ANSI codes to move up.
        # Ensure we clear enough space if line2 shrinks
        # We use a padding to clear the rest of the line if needed, but \033[2K handles it.
        
        sys.stdout.write(f"\033[2K\r{line1}\n\033[2K{line2}\033[A")
        sys.stdout.flush()

class PlatformFilter:
    @staticmethod
    def parse_wheel_platform(filename: str) -> Optional[Tuple[str, str]]:
        if not filename.endswith('.whl'):
            return None
        try:
            # Logic adapted from pypi_package_updater.py
            base = filename[:-4]
            parts = base.split('-')
            if len(parts) < 4:
                return None
            plat = parts[-1]
            if plat == 'any':
                return ('any', 'any')
            
            arch = 'unknown'
            if 'x86_64' in plat or 'amd64' in plat:
                arch = 'amd64'
            elif 'aarch64' in plat or 'arm64' in plat:
                arch = 'arm64'
            elif 'i686' in plat or 'win32' in plat:
                arch = 'x86'
            
            platform = 'other'
            if 'manylinux' in plat or 'linux' in plat or 'musllinux' in plat:
                platform = 'linux'
            elif 'win' in plat:
                platform = 'win'
            elif 'macosx' in plat:
                platform = 'macosx'
            elif 'ios' in plat:
                platform = 'ios'
            return (platform, arch)
        except Exception:
            return None

    @staticmethod
    def should_download(filename: str) -> bool:
        filename = filename.lower()
        # 1. Keep Source packages
        if filename.endswith(('.tar.gz', '.zip')):
            return True
        
        # 2. Check Wheel packages
        if not filename.endswith('.whl'):
            return False

        # Filter: Drop cp27 and pp27
        if '-cp27-' in filename or '-pp27-' in filename:
            return False
            
        # Filter: Drop win32, i686, musllinux
        if 'win32' in filename or 'i686' in filename or 'musllinux' in filename:
            return False

        info = PlatformFilter.parse_wheel_platform(filename)
        if not info:
            # Skip unknown types or non-wheels that aren't source
            return False
            
        platform, arch = info
        
        # 3. Keep 'any' platform
        if platform == 'any':
            return True
            
        # 4. Keep Linux amd64 (Removed x86)
        if platform == 'linux' and arch == 'amd64':
            return True
            
        # 5. Keep Windows amd64 (Removed x86)
        if platform == 'win' and arch == 'amd64':
            return True
            
        # Exclude everything else (MacOS, ARM, etc)
        return False

class SimpleIndexParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.files = {} # filename -> url
        self.current_tag = None
        self.current_href = None

    def handle_starttag(self, tag, attrs):
        if tag == 'a':
            self.current_tag = 'a'
            for k, v in attrs:
                if k == 'href':
                    self.current_href = v
                    break

    def handle_endtag(self, tag):
        if tag == 'a':
            self.current_tag = None
            self.current_href = None

    def handle_data(self, data):
        if self.current_tag == 'a' and self.current_href:
            filename = data.strip()
            if filename:
                self.files[filename] = self.current_href

class RepoComparator:
    def __init__(self, old_root: Path, new_root: Path, downloader: 'Downloader', 
                 download_dir: Path, base_url: str, completed_files: set = None, not_found_files: set = None):
        self.old_root = old_root
        self.new_root = new_root
        self.downloader = downloader
        self.download_dir = download_dir
        self.base_url = base_url.rstrip('/')
        self.completed_files = completed_files or set()
        self.not_found_files = not_found_files or set()
        
        # Logic to handle common mirror structures for 'packages/' paths
        if self.base_url.endswith('/simple'):
            self.root_url = self.base_url[:-7]
        elif self.base_url.endswith('/simple/'):
            self.root_url = self.base_url[:-8]
        else:
            self.root_url = self.base_url

    def parse_index(self, index_path: Path) -> dict:
        if not index_path.exists():
            return {}
        try:
            with open(index_path, 'r', encoding='utf-8') as f:
                content = f.read()
            parser = SimpleIndexParser()
            parser.feed(content)
            return parser.files
        except Exception as e:
            logger.error(f"Failed to parse index {index_path}: {e}")
            return {}

    def process_package(self, package_name: str):
        new_pkg_dir = self.new_root / package_name
        old_pkg_dir = self.old_root / package_name
        
        # Determine index files
        # Usually index.html, but could be just the directory if we were listing files (not supported here)
        # We assume index.html exists
        new_index = new_pkg_dir / "index.html"
        old_index = old_pkg_dir / "index.html"
        
        if not new_index.exists():
            # Maybe it is a file structure without index.html?
            # If so, we can't easily parse metadata unless we listdir.
            # User said "metadata files", implies index.html.
            return

        new_files = self.parse_index(new_index)
        old_files = {}
        
        if old_pkg_dir.exists() and old_index.exists():
            old_files = self.parse_index(old_index)
            
        # Find diff
        # We want files in new but not in old
        # Key is filename
        
        for filename, url in new_files.items():
            if filename not in old_files:
                # Found new file
                if PlatformFilter.should_download(filename):
                    self.submit_download(package_name, filename, url)

    def submit_download(self, package_name: str, filename: str, relative_url: str):
        # Resolve URL
        # The URL in simple index can be:
        # 1. Absolute: https://...
        # 2. Relative to page: ../../packages/... (common in mirrors)
        # 3. Fragment: #sha256=...
        
        # Strip fragment
        url_no_frag = relative_url.split('#')[0]
        
        remote_url = ""
        if relative_url.startswith(('http://', 'https://')):
            remote_url = relative_url
        else:
            # If relative, we need to base it off the simple page URL.
            package_simple_url = f"{self.base_url}/{package_name}/"
            remote_url = urljoin(package_simple_url, relative_url)

        # Local save path
        # User wants to preserve directory structure from metadata (URL)
        # Example: ../../packages/xx/yy/file.whl -> download_dir/packages/xx/yy/file.whl
        
        local_rel_path = ""
        
        if '://' in url_no_frag:
            # Absolute URL
            try:
                parsed = urlparse(url_no_frag)
                path = parsed.path 
                if '/packages/' in path:
                    # Extract part starting from packages/
                    idx = path.find('/packages/')
                    local_rel_path = path[idx+1:] # packages/xx/yy/file.whl
                else:
                    # Fallback: use filename directly in package dir if path structure is unknown
                    local_rel_path = f"{package_name}/{filename}"
            except:
                 local_rel_path = f"{package_name}/{filename}"
        else:
            # Relative URL
            # Clean up ../..
            # We assume relative URLs are relative to simple/pkg/ directory.
            # ../../packages/xx/yy/file.whl -> packages/xx/yy/file.whl
            
            clean_url = url_no_frag
            while clean_url.startswith('../'):
                clean_url = clean_url[3:]
            
            if clean_url.startswith('/'):
                clean_url = clean_url.lstrip('/')
                
            local_rel_path = clean_url
            
            # If path became empty or just filename, maybe put in package dir?
            if not '/' in local_rel_path and local_rel_path == filename:
                 local_rel_path = f"{package_name}/{filename}"

        # Unquote path
        local_rel_path = unquote(local_rel_path)
        local_path = self.download_dir / local_rel_path
        
        # Check against history
        try:
            # Use root_dir of downloader if possible, but here we use download_dir which is root
            rel_path = local_path.relative_to(self.download_dir)
            if str(rel_path) in self.completed_files or str(rel_path) in self.not_found_files:
                return
        except ValueError:
            pass

        self.downloader.add_task((local_path, remote_url))

    def run(self, threads=10):
        # List all packages in new_root
        # Use iterdir
        logger.info(f"Scanning {self.new_root} for packages...")
        try:
            packages = [p.name for p in self.new_root.iterdir() if p.is_dir()]
        except Exception as e:
            logger.error(f"Failed to list packages in {self.new_root}: {e}")
            return

        logger.info(f"Found {len(packages)} packages. Starting comparison with {threads} threads...")
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=threads) as executor:
            # Submit all tasks
            futures = {executor.submit(self.process_package, pkg): pkg for pkg in packages}
            
            completed = 0
            total = len(packages)
            last_log = time.time()
            
            for future in concurrent.futures.as_completed(futures):
                completed += 1
                if time.time() - last_log > 5:
                    logger.info(f"Comparison progress: {completed}/{total} packages processed...")
                    last_log = time.time()
                try:
                    future.result()
                except Exception as e:
                    pkg = futures[future]
                    logger.error(f"Error processing package {pkg}: {e}")

class Downloader:
    def __init__(self, root_dir: Path, num_threads: int = 5, checkpoint_file: Path = None, not_found_file: Path = None, tool: str = 'requests'):
        self.queue = queue.Queue()
        self.num_threads = num_threads
        self.stop_event = threading.Event()
        self.lock = threading.Lock()
        self.success_count = 0
        self.fail_count = 0
        self.not_found_count = 0
        self.total_tasks = 0
        self.processed_tasks = 0
        self.tool = tool
        self.session = requests.Session()
        self.session.headers.update({
            'User-Agent': 'PyPI-Package-Updater/1.0'
        })
        
        # Optimize connection pool
        adapter = requests.adapters.HTTPAdapter(
            pool_connections=num_threads, 
            pool_maxsize=num_threads,
            max_retries=3
        )
        self.session.mount('https://', adapter)
        self.session.mount('http://', adapter)

        self.failed_files = []
        self.checkpoint_file = checkpoint_file
        self.checkpoint_lock = threading.Lock()
        self.not_found_file = not_found_file
        self.not_found_lock = threading.Lock()
        self.root_dir = root_dir
        self.downloaded_bytes = 0
        
        # Track speed per thread
        self.thread_trackers = [{'bytes': 0, 'last_bytes': 0} for _ in range(num_threads)]
        
    def set_total_tasks(self, total: int):
        self.total_tasks = total

    def add_task(self, task):
        with self.lock:
            self.total_tasks += 1
        self.queue.put(task)

    def _log_progress(self):
        with self.lock:
            self.processed_tasks += 1

    def _mark_success(self, local_path: Path):
        with self.lock:
            self.success_count += 1
            
        if self.checkpoint_file:
            with self.checkpoint_lock:
                try:
                    # Save relative path to checkpoint
                    try:
                        rel_path = local_path.relative_to(self.root_dir)
                    except ValueError:
                        rel_path = local_path
                        
                    with open(self.checkpoint_file, "a", encoding="utf-8") as f:
                        f.write(f"{rel_path}\n")
                except Exception as e:
                    logger.error(f"Failed to write checkpoint: {e}")

    def _mark_not_found(self, local_path: Path):
        with self.lock:
            self.not_found_count += 1
            
        if self.not_found_file:
            with self.not_found_lock:
                try:
                    # Save relative path to not_found file
                    try:
                        rel_path = local_path.relative_to(self.root_dir)
                    except ValueError:
                        rel_path = local_path
                        
                    with open(self.not_found_file, "a", encoding="utf-8") as f:
                        f.write(f"{rel_path}\n")
                except Exception as e:
                    logger.error(f"Failed to write not_found file: {e}")

    def worker(self, thread_index):
        while not self.stop_event.is_set():
            try:
                # Use a small timeout to allow checking stop_event frequently
                task = self.queue.get(timeout=0.5)
            except queue.Empty:
                continue
                
            local_path, url = task
            try:
                if self.stop_event.is_set():
                    self.queue.task_done()
                    continue
                    
                self.download_file(local_path, url, thread_index)
                self._mark_success(local_path)
            except requests.exceptions.HTTPError as e:
                # Specific handling for 404 raised from download_file
                if "404 Not Found" in str(e):
                    self._mark_not_found(local_path)
                else:
                    with self.lock:
                        self.fail_count += 1
                        self.failed_files.append(url)
            except OSError as e:
                # Handle Disk Full and Permission errors as Fatal
                if e.errno == errno.ENOSPC: # No space left on device
                    logger.critical(f"FATAL: Disk full while processing {url}. Stopping downloads.")
                    self.stop_event.set()
                    # Add to failed so we know what failed
                    with self.lock:
                        self.fail_count += 1
                        self.failed_files.append(url)
                    break
                elif e.errno in (errno.EACCES, errno.EPERM, errno.EROFS):
                    logger.critical(f"FATAL: Write permission error while processing {url}: {e}. Stopping downloads.")
                    self.stop_event.set()
                    with self.lock:
                        self.fail_count += 1
                        self.failed_files.append(url)
                    break
                else:
                     logger.error(f"IO Error downloading {url}: {e}")
                     with self.lock:
                        self.fail_count += 1
                        self.failed_files.append(url)
            except Exception as e:
                logger.error(f"Failed to download {url}: {e}")
                with self.lock:
                    self.fail_count += 1
                    self.failed_files.append(url)
            finally:
                self._log_progress()
                self.queue.task_done()
    
    def download_file(self, local_path: Path, url: str, thread_index: int):
        # Temp file logic
        # Download to local_path + ".tmp"
        # Only when successful, ensure dir exists and rename
        
        # However, we need to handle resume.
        # If .tmp exists, we resume on .tmp
        # If final file exists, we check if it is complete (or assume complete if we don't have checksum)
        # If we assume complete, we skip.
        
        if local_path.exists():
             # Already downloaded
             logger.info(f"File already exists, skipping: {local_path.name}")
             return

        temp_path = local_path.with_suffix(local_path.suffix + ".tmp")
        
        # Select User-Agent
        user_agent = random.choice(USER_AGENTS)

        # Now we know file exists, create directory
        if not local_path.parent.exists():
            try:
                local_path.parent.mkdir(parents=True, exist_ok=True)
            except FileExistsError:
                pass

        # Use external tool if specified
        if self.tool in ('wget', 'curl', 'aria2'):
            success = False
            # Check 404 before calling external tool?
            # External tools usually handle 404 by returning error code.
            # But we want to distinguish 404 from other errors.
            # Also, we might want to check HEAD to avoid creating directory if 404.
            # But external tools are better at handling connection issues.
            
            # Let's rely on external tool exit code.
            # But aria2c creates .aria2 file.
            
            # For simplicity, let's just try to download.
            # NOTE: We won't get progress updates (bytes downloaded) easily from external tools
            # without parsing their output, which is complex.
            # So speed monitoring won't work for external tools in this implementation.
            
            if self.tool == 'wget':
                success = ExternalDownloader.download_with_wget(url, temp_path, user_agent)
            elif self.tool == 'curl':
                success = ExternalDownloader.download_with_curl(url, temp_path, user_agent)
            elif self.tool == 'aria2':
                success = ExternalDownloader.download_with_aria2(url, temp_path, user_agent)
                
            if success:
                # Rename temp to final
                if temp_path.exists():
                    try:
                        temp_path.replace(local_path)
                        # We don't know size, so we can't update speed stats accurately
                        # But we can update bytes if we check file size
                        file_size = local_path.stat().st_size
                        with self.lock:
                             self.downloaded_bytes += file_size
                             self.thread_trackers[thread_index]['bytes'] += file_size
                    except OSError as e:
                        logger.error(f"Failed to rename {temp_path} to {local_path}: {e}")
                        raise
                else:
                    # Should exist if success
                    raise Exception("Download reported success but file missing")
            else:
                # Check if it was 404? Hard to tell from generic error code.
                # We might need to fallback to requests to check 404 if tool failed.
                # Or just mark as failed.
                # Let's do a quick HEAD check with requests if tool failed, to see if it is 404.
                try:
                    with self.session.head(url, timeout=10) as r:
                         if r.status_code == 404:
                             raise requests.exceptions.HTTPError(f"404 Not Found: {url}")
                except requests.exceptions.HTTPError:
                    raise # Re-raise 404
                except:
                    pass # Ignore other errors, just report tool failure
                
                raise Exception(f"External tool {self.tool} failed to download {url}")
            return

        # Default requests implementation
        
        # Ensure temp directory exists? No, we want to avoid creating target dir if possible.
        # But if we want to support resume on temp file, we need a place to store it.
        # Option 1: Store temp file in system temp dir -> No, cross-device move is slow.
        # Option 2: Store temp file in target dir -> Creates dir.
        # User requirement: "Avoid creating useless directories for 404s".
        # So we should probably download to a common temp dir on the same volume, or just verify URL first?
        # Verifying URL with HEAD is costly.
        
        # Best approach:
        # 1. Determine a temporary directory relative to the root download dir or data dir.
        #    But local_path can be anywhere if absolute paths are used (though unlikely here).
        #    Let's assume we can create a '.downloading' directory in the root of data_dir or download_dir.
        
        # Let's try to find a common root or just use the parent of local_path but only create it if download starts?
        # If we start download, we are committed. If 404 happens, it happens immediately.
        # The issue is `requests.get(stream=True)` returns headers. If 404, we stop.
        # So we can check status code BEFORE creating directory.
        
        resume_header = {}
        mode = 'wb'
        existing_size = 0
        
        # Check if we have a partial download in temp location
        # But where is the temp location?
        # If we don't want to create target dir, we can't put .tmp there yet.
        # But if we don't put .tmp there, we can't easily resume without moving files around.
        # User said "avoid creating useless directories because of 404".
        # So if 404, we don't create dir.
        # If 200, we create dir and download.
        
        # So we can just defer directory creation until we get a 200 OK (or 206 Partial).
        
        # But what about resume?
        # If we want to resume, the file must exist. If file exists, the dir must exist.
        # So if we are resuming, the dir already exists.
        # If we are starting fresh, the dir might not exist.
        
        # So:
        # 1. Check if target file exists (skip if so).
        # 2. Check if .tmp file exists (in target dir). If so, dir exists. We can resume.
        # 3. If neither, we don't know if URL is valid.
        #    We make request.
        #    If 404, we error out, NO dir created.
        #    If 200, we create dir, open .tmp, write.
        
        if temp_path.exists():
            existing_size = temp_path.stat().st_size
            if existing_size > 0:
                resume_header = {'Range': f'bytes={existing_size}-'}
                mode = 'ab'
        
        # Rotate User-Agent
        headers = resume_header.copy()
        headers['User-Agent'] = user_agent

        try:
            # Timeout for connect and read
            with self.session.get(url, headers=headers, stream=True, timeout=(10, 60)) as r:
                if r.status_code == 416: 
                    # Range not satisfiable. 
                    # Assume complete or invalid.
                    logger.debug(f"Range not satisfiable for {url}. Restarting.")
                    resume_header = {}
                    mode = 'wb'
                    existing_size = 0
                    # Retry without range
                    # Recursive call or just fail? Let's just continue with a new request if we could, 
                    # but we are in a context manager. 
                    # Simpler to just delete temp and let retry logic handle it, or just raise exception.
                    # But we can't easily retry here without refactoring.
                    # Let's return and let the worker retry loop handle it?
                    # Worker doesn't have retry loop for logic errors, only exceptions.
                    # Let's raise an exception to trigger worker retry? No, worker counts as fail.
                    
                    # For now, let's just log and fail this attempt.
                    raise requests.exceptions.RequestException("416 Range Not Satisfiable")
                
                if r.status_code == 404:
                    # Do not create directory
                    logger.error(f"404 Not Found: {url}")
                    # If temp file existed (weird if 404), maybe delete it?
                    if temp_path.exists():
                        try:
                            temp_path.unlink()
                        except:
                            pass
                    # Raise exception to record as failure
                    raise requests.exceptions.HTTPError(f"404 Not Found: {url}")

                r.raise_for_status()
                
                # If server doesn't support resume
                if r.status_code == 200 and mode == 'ab':
                    if existing_size > 0:
                        logger.warning(f"Server doesn't support resume for {url} (Got 200), restarting download")
                    mode = 'wb'
                    existing_size = 0
                    # We will truncate when opening file
                
                # Now we know file exists, create directory
                # if not local_path.parent.exists():
                #     try:
                #         local_path.parent.mkdir(parents=True, exist_ok=True)
                #     except FileExistsError:
                #         pass
                # External tool logic already created dir if needed, but for requests we need to ensure it
                if not local_path.parent.exists():
                    try:
                        local_path.parent.mkdir(parents=True, exist_ok=True)
                    except FileExistsError:
                        pass
                
                with open(temp_path, mode) as f:
                    for chunk in r.iter_content(chunk_size=131072): # 128KB chunk size
                        if self.stop_event.is_set():
                            break
                        if chunk:
                            f.write(chunk)
                            chunk_len = len(chunk)
                            with self.lock:
                                self.downloaded_bytes += chunk_len
                                self.thread_trackers[thread_index]['bytes'] += chunk_len
                            
            if self.stop_event.is_set():
                logger.info(f"Download interrupted: {local_path.name}")
                # Cleanup temp file if interrupted
                if temp_path.exists():
                    try:
                        temp_path.unlink()
                        logger.info(f"Cleaned up temp file: {temp_path.name}")
                    except Exception as e:
                        logger.error(f"Failed to cleanup temp file {temp_path}: {e}")
            else:
                # Rename temp to final
                # If target exists now (race?), overwrite
                temp_path.replace(local_path)
                
                if mode == 'ab':
                    logger.info(f"Resumed/Completed: {local_path.name}")
                else:
                    logger.info(f"Downloaded: {local_path.name}")
                
        except requests.exceptions.RequestException as e:
            # logger.error(f"Network error downloading {url}: {e}") # Caller logs this
            raise

    def start(self):
        threads = []
        for i in range(self.num_threads):
            t = threading.Thread(target=self.worker, args=(i,))
            t.start()
            threads.append(t)
        return threads

def main():
    parser = argparse.ArgumentParser(description="Download missing PyPI files based on missing_files lists or directory comparison")
    parser.add_argument("data_dir", nargs='?', help="Path to data directory containing package subdirectories (optional if using comparison mode)")
    parser.add_argument("--base-url", default="https://pypi.tuna.tsinghua.edu.cn/simple", help="Base URL for PyPI simple index (default: https://pypi.tuna.tsinghua.edu.cn/simple)")
    parser.add_argument("--download-dir", help="Optional directory to download files to")
    parser.add_argument("--threads", type=int, default=5, help="Number of download threads")
    parser.add_argument("--force-scan", action="store_true", help="Force rescan of directory even if cache exists")
    parser.add_argument("--tool", default="requests", choices=['requests', 'wget', 'curl', 'aria2'], help="Download tool to use")
    
    # New arguments for comparison mode
    parser.add_argument("--compare-old", help="Path to old simple directory for comparison")
    parser.add_argument("--compare-new", help="Path to new simple directory for comparison")
    
    args = parser.parse_args()

    # Determine mode
    mode = "scan"
    if args.compare_old and args.compare_new:
        mode = "compare"
        old_root = Path(args.compare_old).resolve()
        new_root = Path(args.compare_new).resolve()
        if not old_root.exists():
             logger.error(f"Old directory not found: {old_root}")
             sys.exit(1)
        if not new_root.exists():
             logger.error(f"New directory not found: {new_root}")
             sys.exit(1)
    elif args.data_dir:
        data_dir = Path(args.data_dir).resolve()
        if not data_dir.exists():
            logger.error(f"Directory not found: {data_dir}")
            sys.exit(1)
    else:
        parser.error("Either data_dir or both --compare-old and --compare-new must be specified")

    # Common setup
    if args.download_dir:
        download_dir = Path(args.download_dir).resolve()
    else:
        if mode == "compare":
            download_dir = Path("downloads").resolve()
        else:
             # Default behavior: try to find a sibling 'packages' directory or create 'downloads'
             if data_dir.name == 'simple':
                 download_dir = data_dir.parent / 'packages'
             else:
                 download_dir = Path('downloads').resolve()

    if not download_dir.exists():
        try:
            download_dir.mkdir(parents=True, exist_ok=True)
        except Exception as e:
            logger.error(f"Failed to create download directory {download_dir}: {e}")
            sys.exit(1)
            
    # Determine output directory for logs/cache (prefer download_dir, else data_dir/cwd)
    output_dir = download_dir if download_dir else Path.cwd()
    root_dir = download_dir
    
    cache_file = output_dir / "missing_files_tasks.json"
    checkpoint_file = output_dir / "download_checkpoint.txt"
    not_found_file = output_dir / "not_found_files.txt"

    # Load checkpoint
    completed_files = set()
    if checkpoint_file.exists():
        try:
            with open(checkpoint_file, 'r', encoding='utf-8') as f:
                # Checkpoint stores relative paths
                completed_files = set(line.strip() for line in f if line.strip())
            logger.info(f"Loaded {len(completed_files)} completed tasks from checkpoint.")
        except Exception as e:
            logger.error(f"Failed to load checkpoint: {e}")

    # Load not_found files
    not_found_files = set()
    if not_found_file.exists():
        try:
            with open(not_found_file, 'r', encoding='utf-8') as f:
                not_found_files = set(line.strip() for line in f if line.strip())
            logger.info(f"Loaded {len(not_found_files)} not found tasks from history.")
        except Exception as e:
            logger.error(f"Failed to load not_found file: {e}")

    # Initialize Downloader
    downloader = Downloader(root_dir=root_dir, num_threads=args.threads, checkpoint_file=checkpoint_file, not_found_file=not_found_file, tool=args.tool)
    threads = downloader.start()
    
    # Start status printer
    status_printer = StatusPrinter(downloader)
    status_printer.start()

    try:
        if mode == "compare":
            comparator = RepoComparator(old_root, new_root, downloader, download_dir, args.base_url, completed_files, not_found_files)
            comparator.run(threads=args.threads)
        else:
            # tasks will store raw info: (line_from_file, package_name)
            raw_tasks = []
            
            # Try load from cache
            if not args.force_scan and cache_file.exists():
                logger.info(f"Loading task list from cache: {cache_file}")
                try:
                    with open(cache_file, 'r', encoding='utf-8') as f:
                        loaded_data = json.load(f)
                        # Check format compatibility
                        if loaded_data and isinstance(loaded_data[0], list) and len(loaded_data[0]) == 2:
                            # New format: [line, package_name]
                            raw_tasks = loaded_data
                        else:
                            logger.warning("Cache format mismatch or empty, ignoring.")
                    logger.info(f"Loaded {len(raw_tasks)} raw tasks from cache.")
                except Exception as e:
                    logger.error(f"Failed to load cache: {e}. Will rescan.")
                    raw_tasks = []

            # If no tasks loaded, scan
            if not raw_tasks:
                # Scan directories recursively
                logger.info(f"Scanning {data_dir} for missing_files/missing_files.txt recursively...")
                
                candidates = []
                scanned_dirs = 0
                last_scan_log = time.time()
                
                for root, dirs, files in os.walk(data_dir):
                    scanned_dirs += 1
                    if time.time() - last_scan_log > 10:
                        logger.info(f"Scanning progress: Checked {scanned_dirs} directories, found {len(candidates)} candidate files...")
                        last_scan_log = time.time()
                        
                    if "missing_files" in files:
                        candidates.append(Path(root) / "missing_files")
                    if "missing_files.txt" in files:
                        candidates.append(Path(root) / "missing_files.txt")

                # Remove duplicates if any (though paths should be unique)
                candidates = sorted(list(set(candidates)))
                logger.info(f"Scan complete. Found {len(candidates)} candidate files in {scanned_dirs} directories.")

                total_candidates = len(candidates)
                processed_count = 0
                last_process_log = time.time()

                for missing_file in candidates:
                    processed_count += 1
                    if time.time() - last_process_log > 10:
                        logger.info(f"Processing candidates: {processed_count}/{total_candidates}...")
                        last_process_log = time.time()
                    if not missing_file.is_file():
                        continue
                        
                    package_dir = missing_file.parent
                    package_name = package_dir.name
                    
                    try:
                        with open(missing_file, 'r', encoding='utf-8') as f:
                            for line in f:
                                line = line.strip()
                                if not line:
                                    continue
                                    
                                # Filter based on filename
                                filename = os.path.basename(line)
                                if PlatformFilter.should_download(filename):
                                     # Store raw info: relative path line and package name
                                     raw_tasks.append((line, package_name))
                    except Exception as e:
                        logger.error(f"Error reading {missing_file}: {e}")
                
                # Save cache
                try:
                    logger.info(f"Saving {len(raw_tasks)} raw tasks to cache: {cache_file}")
                    with open(cache_file, 'w', encoding='utf-8') as f:
                        # Save as list of [line, package_name]
                        json.dump(raw_tasks, f)
                except Exception as e:
                    logger.error(f"Failed to save cache: {e}")

            # Process tasks to generate execution list
            base_url = args.base_url.rstrip('/')
            
            # Pre-calculate root_url for packages/ paths
            from urllib.parse import urlparse, unquote
            
            if base_url.endswith('/simple'):
                root_url = base_url[:-7]
            elif base_url.endswith('/simple/'):
                root_url = base_url[:-8]
            else:
                root_url = base_url

            logger.info(f"Processing {len(raw_tasks)} raw tasks and downloading in parallel...")

            skipped_count = 0
            for line, package_name in raw_tasks:
                # Determine local save path
                if download_dir:
                    target_root = download_dir
                else:
                    # If no download dir, save relative to package dir
                    target_root = data_dir / package_name
                    
                # Determine remote URL
                if line.startswith(('http://', 'https://')):
                    remote_url = line
                else:
                    if line.startswith('packages/'):
                        remote_url = urljoin(root_url + '/', line)
                    else:
                        # Fallback to package relative (old behavior)
                        remote_url = urljoin(f"{base_url}/{package_name}/", line)
                    
                    # Unquote the local path
                    local_save_path = target_root / unquote(line)
                
                # Check against checkpoint
                try:
                    rel_path = local_save_path.relative_to(root_dir)
                    if str(rel_path) in completed_files or str(rel_path) in not_found_files:
                        skipped_count += 1
                        continue
                except ValueError:
                    pass
                    
                # Add to downloader immediately
                downloader.add_task((local_save_path, remote_url))
                
            logger.info(f"Total raw tasks: {len(raw_tasks)}, Skipped (Checkpoint): {skipped_count}, Queued: {downloader.total_tasks}")

        if downloader.total_tasks == 0:
            logger.info("No tasks to process.")
            downloader.stop_event.set()
            status_printer.stop_event.set()
            for t in threads:
                t.join()
            status_printer.join()
            return

        # Wait for queue to be empty
        while not downloader.queue.empty() or any(t.is_alive() for t in threads):
            if downloader.stop_event.is_set():
                 break

            if downloader.queue.empty():
                 # Wait a bit for threads to finish last tasks
                 time.sleep(0.5)
                 if all(not t.is_alive() for t in threads):
                     break
            else:
                time.sleep(0.5)
                
    except KeyboardInterrupt:
        sys.stdout.write("\nStopping... (Please wait for current downloads to finish or Ctrl+C again to force kill)\n")
        downloader.stop_event.set()
        # Clear queue to speed up shutdown
        try:
            while True:
                downloader.queue.get_nowait()
                downloader.queue.task_done()
        except queue.Empty:
            pass
    
    # Ensure threads join
    downloader.stop_event.set() # Ensure workers exit
    status_printer.stop_event.set()
    
    # Wait for threads with timeout
    for t in threads:
        t.join(timeout=2.0)
    
    status_printer.join(timeout=1.0)
        
    if downloader.failed_files:
        logger.error(f"Failed files summary ({len(downloader.failed_files)} files):")
        for f in downloader.failed_files:
            logger.error(f" - {f}")

    logger.info(f"Done. Success: {downloader.success_count}, Failed: {downloader.fail_count}")

if __name__ == "__main__":
    main()
