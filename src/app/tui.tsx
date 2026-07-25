import React, {useEffect, useMemo, useState} from 'react';
import {Box, Text, render, useApp, useInput, useStdin} from 'ink';
import TextInput from 'ink-text-input';

import {DEFAULT_CONFIG_PATH, defaultConfig, loadConfig, normalizeConfig, saveConfig, taskLabel} from './config.js';
import {runSync, type SyncEvent} from './run-sync.js';
import {TaskController} from './task-controller.js';
import type {
  AppConfig,
  BaseAppConfig,
  IncrementalDownloadTaskConfig,
  MetadataSyncTaskConfig,
  PypiTaskType,
  SyncRunResult,
  ArtifactDownloadTaskConfig
} from '../shared/types.js';

type Screen = 'provider' | 'task' | 'config' | 'confirm';
type ConfigSection = 'base' | 'task';

interface BaseFieldDefinition {
  key: keyof BaseAppConfig;
  label: string;
}

type TaskFieldKey = keyof MetadataSyncTaskConfig | keyof ArtifactDownloadTaskConfig | keyof IncrementalDownloadTaskConfig;

interface TaskFieldDefinition {
  key: TaskFieldKey;
  label: string;
}

interface TaskDefinition {
  id: PypiTaskType;
  label: string;
  description: string;
  taskFields: TaskFieldDefinition[];
}

const PROVIDERS = [{id: 'pypi', label: 'PyPI', description: '同步 simple 元数据与 packages 包文件。'}] as const;

const BASE_FIELDS: BaseFieldDefinition[] = [
  {key: 'profileName', label: 'Profile Name'},
  {key: 'simpleUrl', label: 'Simple URL'},
  {key: 'metadataRoot', label: 'Metadata Root'},
  {key: 'mirrorRoot', label: 'Mirror Root'},
  {key: 'concurrency', label: 'Concurrency'},
  {key: 'retry', label: 'Retry'},
  {key: 'timeoutMs', label: 'Timeout (ms)'}
];

const TASK_DEFINITIONS: TaskDefinition[] = [
  {
    id: 'metadata-sync',
    label: '下载元数据',
    description: '将当日 PyPI 元数据下载到 metadataRoot/snapshots/pypi-<日期>/。',
    taskFields: [{key: 'snapshotDate', label: 'Metadata Date'}]
  },
  {
    id: 'artifact-download',
    label: '按单日元数据下载包',
    description: '读取某个日期的元数据目录，下载包到 mirrorRoot/pypi-<日期>/packages/...。',
    taskFields: [
      {key: 'metadataDate', label: 'Source Metadata Date'},
      {key: 'outputDate', label: 'Mirror Output Date'}
    ]
  },
  {
    id: 'incremental-download',
    label: '按两日元数据增量下载',
    description: '比较两份日期元数据，只下载新增或变更包到 mirrorRoot/pypi-<日期>/packages/...。',
    taskFields: [
      {key: 'oldMetadataDate', label: 'Old Metadata Date'},
      {key: 'newMetadataDate', label: 'New Metadata Date'},
      {key: 'outputDate', label: 'Mirror Output Date'}
    ]
  }
];

function appendLog(logs: string[], line: string): string[] {
  return [...logs, line].slice(-14);
}

function getTaskDefinition(taskType: PypiTaskType): TaskDefinition {
  return TASK_DEFINITIONS.find((task) => task.id === taskType) ?? TASK_DEFINITIONS[0]!;
}

function readTaskField(config: AppConfig, taskType: PypiTaskType, field: TaskFieldDefinition): string {
  switch (taskType) {
    case 'metadata-sync':
      return config.pypi.metadataSync[field.key as keyof MetadataSyncTaskConfig] ?? '';
    case 'artifact-download':
      return config.pypi.artifactDownload[field.key as keyof ArtifactDownloadTaskConfig] ?? '';
    case 'incremental-download':
      return config.pypi.incrementalDownload[field.key as keyof IncrementalDownloadTaskConfig] ?? '';
  }
}

function writeTaskField(config: AppConfig, taskType: PypiTaskType, field: TaskFieldDefinition, value: string): AppConfig {
  switch (taskType) {
    case 'metadata-sync':
      return {
        ...config,
        pypi: {
          ...config.pypi,
          metadataSync: {
            ...config.pypi.metadataSync,
            [field.key]: value
          }
        }
      };
    case 'artifact-download':
      return {
        ...config,
        pypi: {
          ...config.pypi,
          artifactDownload: {
            ...config.pypi.artifactDownload,
            [field.key]: value
          }
        }
      };
    case 'incremental-download':
      return {
        ...config,
        pypi: {
          ...config.pypi,
          incrementalDownload: {
            ...config.pypi.incrementalDownload,
            [field.key]: value
          }
        }
      };
  }
}

function InteractiveApp(): React.JSX.Element {
  const {exit} = useApp();
  const [config, setConfig] = useState<AppConfig>(defaultConfig());
  const [screen, setScreen] = useState<Screen>('provider');
  const [providerIndex, setProviderIndex] = useState(0);
  const [taskIndex, setTaskIndex] = useState(0);
  const [configSection, setConfigSection] = useState<ConfigSection>('base');
  const [baseFieldIndex, setBaseFieldIndex] = useState(0);
  const [taskFieldIndex, setTaskFieldIndex] = useState(0);
  const [editingField, setEditingField] = useState<string | undefined>();
  const [draftValue, setDraftValue] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [running, setRunning] = useState(false);
  const [status, setStatus] = useState('Loading config...');
  const [logs, setLogs] = useState<string[]>([]);
  const [lastResult, setLastResult] = useState<SyncRunResult | undefined>();
  const [taskController, setTaskController] = useState<TaskController | undefined>();
  const [progressData, setProgressData] = useState<{current: number; total: number; failed: number} | undefined>();

  const selectedTask = TASK_DEFINITIONS[taskIndex] ?? TASK_DEFINITIONS[0]!;
  const baseField = BASE_FIELDS[baseFieldIndex];
  const taskField = selectedTask.taskFields[taskFieldIndex];
  const isEditing = Boolean(editingField);

  useEffect(() => {
    void (async () => {
      const loadedConfig = await loadConfig();
      setConfig(loadedConfig);
      const currentTaskIndex = TASK_DEFINITIONS.findIndex((task) => task.id === loadedConfig.selectedTask);
      setTaskIndex(currentTaskIndex >= 0 ? currentTaskIndex : 0);
      setLoading(false);
      setStatus(`Config loaded from ${DEFAULT_CONFIG_PATH}`);
    })();
  }, []);

  const saveCurrentConfig = async (): Promise<void> => {
    setSaving(true);
    try {
      const normalized = normalizeConfig(config);
      setConfig(normalized);
      await saveConfig(normalized);
      setStatus(`Config saved to ${DEFAULT_CONFIG_PATH}`);
      setLogs((current) => appendLog(current, `[config] saved ${DEFAULT_CONFIG_PATH}`));
    } finally {
      setSaving(false);
    }
  };

  const startBaseFieldEdit = (): void => {
    if (!baseField) {
      return;
    }

    setEditingField(`base:${String(baseField.key)}`);
    setDraftValue(String(config.base[baseField.key]));
    setStatus(`Editing base config: ${baseField.label}`);
  };

  const startTaskFieldEdit = (): void => {
    if (!taskField) {
      return;
    }

    setEditingField(`task:${String(taskField.key)}`);
    setDraftValue(readTaskField(config, selectedTask.id, taskField));
    setStatus(`Editing task config: ${taskField.label}`);
  };

  const runCurrentTask = async (): Promise<void> => {
    setRunning(true);
    setLastResult(undefined);
    setProgressData(undefined);
    const controller = new TaskController();
    setTaskController(controller);
    setLogs((current) => appendLog(current, `[run] provider=pypi task=${config.selectedTask}`));

    try {
      const normalized = normalizeConfig(config);
      setConfig(normalized);
      await saveConfig(normalized);
      const result = await runSync({
        config: normalized,
        taskController: controller,
        onEvent: (event: SyncEvent) => {
          setStatus(`${event.stage}: ${event.message}`);
          if (event.progress) {
            setProgressData(event.progress);
          } else {
            setLogs((current) => appendLog(current, `[${event.stage}] ${event.message}`));
          }
        }
      });
      setLastResult(result);
      setStatus(`Task completed: ${taskLabel(result.taskType)}`);
      setLogs((current) => appendLog(current, `[done] ${taskLabel(result.taskType)}`));
      setScreen('provider');
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      setStatus(`Task failed: ${message}`);
      setLogs((current) => appendLog(current, `[error] ${message}`));
    } finally {
      setRunning(false);
      setTaskController(undefined);
    }
  };

  useInput((input, key) => {
    if (key.ctrl && input === 'c') {
      exit();
      return;
    }

    if (running) {
      if (input === 'p') {
        if (taskController) {
          if (taskController.isPaused()) {
            taskController.resume();
            setStatus('Resumed');
          } else {
            taskController.pause();
            setStatus('Paused');
          }
        }
        return;
      }
      if (input === 'c') {
        if (taskController) {
          taskController.cancel();
          setStatus('Cancelling...');
        }
        return;
      }
      if (input === 'q') {
        if (taskController) {
          taskController.cancel();
        }
        exit();
      }
      return;
    }

    if (isEditing) {
      if (key.escape) {
        setEditingField(undefined);
        setDraftValue('');
        setStatus('Edit cancelled');
      }
      return;
    }

    if (input === 'q') {
      exit();
      return;
    }

    if (screen === 'provider') {
      if (key.upArrow || key.downArrow) {
        setProviderIndex(0);
        return;
      }

      if (key.return || input === 'n') {
        setScreen('task');
        setStatus('Provider selected: PyPI');
      }
      return;
    }

    if (screen === 'task') {
      if (key.upArrow) {
        setTaskIndex((current) => (current - 1 + TASK_DEFINITIONS.length) % TASK_DEFINITIONS.length);
        return;
      }

      if (key.downArrow) {
        setTaskIndex((current) => (current + 1) % TASK_DEFINITIONS.length);
        return;
      }

      if (key.return || input === 'n') {
        setConfig((current) => ({
          ...current,
          selectedTask: selectedTask.id
        }));
        setConfigSection('base');
        setTaskFieldIndex(0);
        setScreen('config');
        setStatus(`Task selected: ${selectedTask.label}`);
        return;
      }

      if (input === 'b') {
        setScreen('provider');
      }
      return;
    }

    if (screen === 'config') {
      if (input === 'b') {
        setScreen('task');
        setStatus('Back to task selection');
        return;
      }

      if (input === '\t') {
        setConfigSection((current) => (current === 'base' ? 'task' : 'base'));
        return;
      }

      if (input === 'h') {
        setConfigSection('base');
        return;
      }

      if (input === 'l') {
        setConfigSection('task');
        return;
      }

      if (configSection === 'base') {
        if (key.upArrow) {
          setBaseFieldIndex((current) => (current - 1 + BASE_FIELDS.length) % BASE_FIELDS.length);
          return;
        }
        if (key.downArrow) {
          setBaseFieldIndex((current) => (current + 1) % BASE_FIELDS.length);
          return;
        }
        if (key.return || input === 'e') {
          startBaseFieldEdit();
          return;
        }
      } else {
        if (key.upArrow) {
          setTaskFieldIndex((current) => (current - 1 + selectedTask.taskFields.length) % selectedTask.taskFields.length);
          return;
        }
        if (key.downArrow) {
          setTaskFieldIndex((current) => (current + 1) % selectedTask.taskFields.length);
          return;
        }
        if (key.return || input === 'e') {
          startTaskFieldEdit();
          return;
        }
      }

      if (input === 's') {
        void saveCurrentConfig();
        return;
      }

      if (input === 'c') {
        setScreen('confirm');
        setStatus('Please confirm the configuration before starting');
      }
      return;
    }

    if (screen === 'confirm') {
      if (input === 'b' || key.leftArrow) {
        setScreen('config');
        setStatus('Back to config editor');
        return;
      }

      if (input === 's') {
        void saveCurrentConfig();
        return;
      }

      if (key.return || input === 'r') {
        void runCurrentTask();
      }
    }
  });

  const summaryLines = useMemo(() => {
    if (!lastResult) {
      return [];
    }

    return [
      `Provider: ${lastResult.provider}`,
      `Task: ${taskLabel(lastResult.taskType)}`,
      ...(lastResult.snapshotId ? [`Snapshot: ${lastResult.snapshotId}`] : []),
      ...(lastResult.packageCount !== undefined ? [`Packages: ${lastResult.packageCount}`] : []),
      ...(lastResult.manifest ? [`Artifacts: ${lastResult.manifest.stats.artifactsTotal}`] : []),
      ...(lastResult.plan ? [`Plan entries: ${lastResult.plan.entries.length}`] : []),
      ...(lastResult.diff ? [`Added: ${lastResult.diff.added.length}`, `Changed: ${lastResult.diff.changed.length}`] : []),
      ...(lastResult.downloadSummary
        ? [`Downloaded: ${lastResult.downloadSummary.downloaded}`, `Failed: ${lastResult.downloadSummary.failed.length}`]
        : []),
      ...(lastResult.outputRoot ? [`Output Root: ${lastResult.outputRoot}`] : [])
    ];
  }, [lastResult]);

  const renderProgressBar = () => {
    if (!progressData || progressData.total === 0) {
      return null;
    }
    const {current, total, failed} = progressData;
    const percent = Math.min(100, Math.max(0, Math.floor((current / total) * 100)));
    const barWidth = 40;
    const filled = Math.floor((percent / 100) * barWidth);
    const empty = barWidth - filled;
    const bar = '█'.repeat(filled) + '░'.repeat(empty);
    return (
      <Box flexDirection="column" marginTop={1}>
        <Text color="yellow">Download Progress</Text>
        <Box>
          <Text color={taskController?.isPaused() ? "yellow" : "cyan"}>{bar}</Text>
          <Text> {percent}% </Text>
          <Text dimColor>({current}/{total})</Text>
          {failed > 0 && <Text color="red"> Failed: {failed}</Text>}
        </Box>
        {taskController?.isPaused() && <Text color="yellow">Task is PAUSED</Text>}
      </Box>
    );
  };

  return (
    <Box flexDirection="column" padding={1} width={80}>
      <Box borderStyle="round" borderColor="cyan" paddingX={2} paddingY={1} flexDirection="column">
        <Text color="cyan" bold>mirror-sync TUI</Text>
        <Text dimColor>Step 1: Provider ➔ Step 2: Task ➔ Step 3: Config ➔ Step 4: Run</Text>
      </Box>

      <Box marginTop={1} flexDirection="row">
        {/* Left Column: Provider & Task */}
        <Box flexDirection="column" width="40%" paddingRight={2}>
          <Text color="yellow" bold>Provider</Text>
          {screen === 'provider' ? (
            PROVIDERS.map((provider, index) => (
              <Box key={provider.id} flexDirection="column" marginTop={1}>
                <Box>
                  {index === providerIndex ? <Text color="green" bold>&gt; {provider.label}</Text> : <Text>  {provider.label}</Text>}
                </Box>
                <Text dimColor>  {provider.description}</Text>
              </Box>
            ))
          ) : (
            <Text color="green">✔ PyPI</Text>
          )}

          <Box marginTop={2}>
            <Text color="yellow" bold>Task</Text>
          </Box>
          {screen === 'task' ? (
            TASK_DEFINITIONS.map((task, index) => (
              <Box key={task.id} flexDirection="column" marginTop={1}>
                <Box>
                  {index === taskIndex ? <Text color="green" bold>&gt; {task.label}</Text> : <Text>  {task.label}</Text>}
                </Box>
                <Text dimColor>  {task.description}</Text>
              </Box>
            ))
          ) : screen !== 'provider' ? (
            <Text color="green">✔ {getTaskDefinition(config.selectedTask).label}</Text>
          ) : (
            <Text dimColor>Pending...</Text>
          )}
        </Box>

        {/* Right Column: Config & Confirm */}
        <Box flexDirection="column" width="60%">
          <Text color="yellow" bold>Configuration</Text>
          {screen === 'provider' || screen === 'task' ? (
            <Text dimColor>Select provider and task first...</Text>
          ) : (
            <Box flexDirection="column" marginTop={1}>
              <Text color="cyan" underline>Base Settings</Text>
              {BASE_FIELDS.map((field, index) => {
                const selected = screen === 'config' && configSection === 'base' && index === baseFieldIndex;
                const editing = editingField === `base:${String(field.key)}`;
                return (
                  <Box key={String(field.key)}>
                    <Box width={2}>
                      {selected ? <Text color="green" bold>&gt;</Text> : <Text> </Text>}
                    </Box>
                    <Box width={18}>
                      <Text>{field.label}:</Text>
                    </Box>
                    {editing ? (
                      <TextInput
                        value={draftValue}
                        onChange={setDraftValue}
                        onSubmit={(value) => {
                          setConfig((current) => ({
                            ...current,
                            base: {
                              ...current.base,
                              [field.key]:
                                field.key === 'concurrency' || field.key === 'retry' || field.key === 'timeoutMs'
                                  ? Number.isFinite(Number(value.trim()))
                                    ? Number(value.trim())
                                    : current.base[field.key]
                                  : value
                            }
                          }));
                          setEditingField(undefined);
                          setDraftValue('');
                          setStatus(`Updated base config: ${field.label}`);
                        }}
                      />
                    ) : selected ? (
                      <Text color="green">{String(config.base[field.key])}</Text>
                    ) : (
                      <Text>{String(config.base[field.key])}</Text>
                    )}
                  </Box>
                );
              })}

              <Box marginTop={1}>
                <Text color="cyan" underline>Task Settings</Text>
              </Box>
              {getTaskDefinition(config.selectedTask).taskFields.map((field, index) => {
                const selected = screen === 'config' && configSection === 'task' && index === taskFieldIndex;
                const editing = editingField === `task:${String(field.key)}`;
                return (
                  <Box key={String(field.key)}>
                    <Box width={2}>
                      {selected ? <Text color="green" bold>&gt;</Text> : <Text> </Text>}
                    </Box>
                    <Box width={18}>
                      <Text>{field.label}:</Text>
                    </Box>
                    {editing ? (
                      <TextInput
                        value={draftValue}
                        onChange={setDraftValue}
                        onSubmit={(value) => {
                          setConfig((current) => writeTaskField(current, config.selectedTask, field, value));
                          setEditingField(undefined);
                          setDraftValue('');
                          setStatus(`Updated task config: ${field.label}`);
                        }}
                      />
                    ) : selected ? (
                      <Text color="green">{readTaskField(config, config.selectedTask, field)}</Text>
                    ) : (
                      <Text>{readTaskField(config, config.selectedTask, field)}</Text>
                    )}
                  </Box>
                );
              })}
            </Box>
          )}

          {screen === 'confirm' && (
            <Box marginTop={2} borderStyle="single" borderColor="green" paddingX={1} flexDirection="column">
              <Text color="green" bold>Ready to Run</Text>
              <Text>Press <Text color="whiteBright" bold>Enter</Text> or <Text color="whiteBright" bold>r</Text> to start.</Text>
            </Box>
          )}
        </Box>
      </Box>

      {running && renderProgressBar()}

      <Box marginTop={2} borderStyle="round" borderColor="gray" paddingX={1} flexDirection="column">
        <Text color="yellow" bold>Status Console</Text>
        <Text>{loading ? 'Loading...' : status}</Text>
        {logs.slice(-3).map((line, index) => <Text dimColor key={`${index}-${line}`}>{line}</Text>)}
      </Box>

      <Box marginTop={1} flexDirection="row" justifyContent="space-between">
        <Text dimColor>
          {running
            ? 'Keys: [p] Pause/Resume, [c] Cancel, [q] Quit'
            : screen === 'config'
            ? 'Keys: [↑/↓] Select, [Enter/e] Edit, [Tab/h/l] Switch Section, [c] Confirm, [s] Save, [b] Back, [q] Quit'
            : 'Keys: [↑/↓] Select, [Enter/n] Next, [b] Back, [q] Quit'}
        </Text>
      </Box>
    </Box>
  );
}

function App(): React.JSX.Element {
  const {isRawModeSupported} = useStdin();

  if (!isRawModeSupported) {
    return (
      <Box flexDirection="column" padding={1}>
        <Text color="cyan">mirror-sync</Text>
        <Text>This TUI needs an interactive terminal.</Text>
        <Text dimColor>Run `npm start` directly in a local terminal session.</Text>
        <Text dimColor>Config file: {DEFAULT_CONFIG_PATH}</Text>
      </Box>
    );
  }

  return <InteractiveApp />;
}

render(<App />);
