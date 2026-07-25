import requests
import re
import os
import time
import concurrent.futures
import sys
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

# Configuration
MIRROR_URL = "https://pypi.tuna.tsinghua.edu.cn/simple/"
OUTPUT_DIR = os.path.join(os.getcwd(), "simple") # Use absolute path to be safe
PACKAGE_LIST_FILE = "pypi_packages_list.txt"
MAX_WORKERS = 16  # Concurrency level
TIMEOUT = 30      # Request timeout

def get_session():
    """Create a requests Session with retry logic."""
    session = requests.Session()
    # Retry strategy: 3 retries, exponential backoff
    retries = Retry(
        total=3,
        backoff_factor=0.5,
        status_forcelist=[500, 502, 503, 504, 429],
        allowed_methods=["GET"]
    )
    adapter = HTTPAdapter(max_retries=retries, pool_connections=MAX_WORKERS, pool_maxsize=MAX_WORKERS)
    session.mount("http://", adapter)
    session.mount("https://", adapter)
    
    # Headers to mimic browser
    session.headers.update({
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    })
    return session

def fetch_all_pypi_packages(mirror_url=MIRROR_URL):
    """Fetch the list of all packages from the simple index."""
    print(f"正在从 {mirror_url} 获取全量包列表...")
    
    session = get_session()
    try:
        response = session.get(mirror_url, timeout=120)
        response.raise_for_status()
        
        html_content = response.text
        print(f"获取成功，页面大小: {len(html_content)/1024/1024:.2f} MB")
        print("正在解析数据 (正则匹配)...")
        
        package_names = re.findall(r'>([^<]+)</a>', html_content)
        
        # Save to file
        with open(PACKAGE_LIST_FILE, "w", encoding="utf-8") as f:
            for name in package_names:
                f.write(name + "\n")
                
        print(f"--- 列表更新完成 ---")
        print(f"共发现 {len(package_names)} 个包。")
        print(f"已保存到: {PACKAGE_LIST_FILE}")
        return package_names

    except Exception as e:
        print(f"获取包列表失败: {e}")
        if os.path.exists(PACKAGE_LIST_FILE):
            print("尝试使用本地缓存的包列表...")
            try:
                with open(PACKAGE_LIST_FILE, "r", encoding="utf-8") as f:
                    return [line.strip() for line in f if line.strip()]
            except Exception as local_e:
                print(f"读取本地列表失败: {local_e}")
                return []
        else:
            return []

def download_file(session, url, filepath, headers=None):
    """
    Download a single file.
    Returns: "OK", "SKIPPED", "NOT_FOUND", or "ERROR: ..."
    """
    # Resume capability: Skip if file exists and has content
    if os.path.exists(filepath) and os.path.getsize(filepath) > 0:
        return "SKIPPED"

    try:
        # Ensure directory exists (thread-safe enough for os.makedirs)
        os.makedirs(os.path.dirname(filepath), exist_ok=True)

        response = session.get(url, headers=headers, timeout=TIMEOUT)
        if response.status_code == 404:
            return "NOT_FOUND"
        response.raise_for_status()
        
        with open(filepath, "wb") as f:
            for chunk in response.iter_content(chunk_size=8192):
                f.write(chunk)
        return "OK"
    except Exception as e:
        # Clean up partial file if it was created
        if os.path.exists(filepath) and os.path.getsize(filepath) == 0:
            try:
                os.remove(filepath)
            except:
                pass
        return f"ERROR: {e}"

def process_package(package_name, session):
    """Download HTML and JSON metadata for a package."""
    package_dir = os.path.join(OUTPUT_DIR, package_name)
    
    # Base URL for the package
    # Note: mirror_url usually ends with /, so we append package_name/
    base_url = f"{MIRROR_URL}{package_name}/"
    
    # 1. Download HTML (index.html)
    html_path = os.path.join(package_dir, "index.html")
    html_headers = {"Accept": "text/html"} 
    res_html = download_file(session, base_url, html_path, headers=html_headers)
    
    # 2. Download JSON (index_v1.json) - PEP 691
    json_path = os.path.join(package_dir, "index_v1.json")
    json_headers = {"Accept": "application/vnd.pypi.simple.v1+json"}
    res_json = download_file(session, base_url, json_path, headers=json_headers)
    
    return package_name, res_html, res_json

def main():
    # 1. Get Package List
    packages = []
    if os.path.exists(PACKAGE_LIST_FILE):
        print(f"发现本地包列表 {PACKAGE_LIST_FILE}")
        # Ask user or just proceed? For automation, we'll just proceed.
        # But we should offer a way to force update? 
        # The user wants to handle interrupts, so relying on file is good.
        print("读取列表中...")
        with open(PACKAGE_LIST_FILE, "r", encoding="utf-8") as f:
            packages = [line.strip() for line in f if line.strip()]
        print(f"已加载 {len(packages)} 个包。")
    
    if not packages:
        print("本地无列表，开始拉取全量列表...")
        packages = fetch_all_pypi_packages()

    if not packages:
        print("没有找到包，退出。")
        return

    # 2. Prepare for Download
    if not os.path.exists(OUTPUT_DIR):
        os.makedirs(OUTPUT_DIR)
    
    print(f"开始下载元数据到: {OUTPUT_DIR}")
    print(f"并发线程数: {MAX_WORKERS}")
    print("按 Ctrl+C 可以安全停止...")
    
    session = get_session()
    
    total = len(packages)
    count = 0
    success = 0
    errors = 0
    skipped = 0
    
    start_time = time.time()
    
    # Use a set to keep track of active futures to manage memory/queue size
    pending = set()
    
    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=MAX_WORKERS) as executor:
            # Iterator for packages
            pkg_iter = iter(packages)
            
            # Initial fill
            for _ in range(MAX_WORKERS * 2):
                try:
                    pkg = next(pkg_iter)
                    future = executor.submit(process_package, pkg, session)
                    pending.add(future)
                except StopIteration:
                    break
            
            while pending:
                # Wait for at least one future to complete
                done, pending = concurrent.futures.wait(pending, return_when=concurrent.futures.FIRST_COMPLETED)
                
                for future in done:
                    count += 1
                    try:
                        name, h_res, j_res = future.result()
                        
                        # Determine status
                        # We consider it success if at least HTML is OK or SKIPPED
                        # Errors in JSON might be expected if mirror doesn't support it for some pkg
                        
                        if h_res.startswith("ERROR") or j_res.startswith("ERROR"):
                            errors += 1
                        elif h_res == "SKIPPED" and j_res == "SKIPPED":
                            skipped += 1
                        else:
                            success += 1
                            
                    except Exception as exc:
                        errors += 1
                    
                    # Print progress
                    if count % 50 == 0 or count == total:
                        elapsed = time.time() - start_time
                        speed = count / elapsed if elapsed > 0 else 0
                        sys.stdout.write(f"\r进度: {count}/{total} ({count/total*100:.2f}%) | 成功: {success} | 跳过: {skipped} | 错误: {errors} | 速度: {speed:.1f} 包/秒")
                        sys.stdout.flush()

                    # Submit next task
                    try:
                        pkg = next(pkg_iter)
                        future = executor.submit(process_package, pkg, session)
                        pending.add(future)
                    except StopIteration:
                        pass

    except KeyboardInterrupt:
        print("\n\n用户中断! 正在停止所有任务...")
        # Allow threads to exit? Executor context exit will wait.
        # We just break and let the context manager handle cleanup (wait=True by default)
        # But to exit fast, we might just want to exit.
        print("已收到中断信号，正在等待当前下载完成...")
    except Exception as e:
        print(f"\n\n发生异常: {e}")
    
    print(f"\n\n--- 任务结束 ---")
    print(f"总耗时: {time.time() - start_time:.2f} 秒")
    print(f"处理总数: {count}")
    print(f"成功/更新: {success}")
    print(f"跳过(已存在): {skipped}")
    print(f"错误: {errors}")

if __name__ == "__main__":
    # Optional: Allow command line args to force update list
    if len(sys.argv) > 1 and sys.argv[1] == "--update-list":
        fetch_all_pypi_packages()
    main()
