import React, {useEffect, useMemo, useState} from 'react';
import {Box, Text, render, useApp, useInput, useStdin} from 'ink';
import TextInput from 'ink-text-input';

import {DEFAULT_CONFIG_PATH, defaultConfig, loadConfig, normalizeConfig, saveConfig} from './config.js';
import {runSync, type SyncEvent} from './run-sync.js';
import type {AppConfig, SyncRunResult} from '../shared/types.js';

type FieldType = 'text' | 'number' | 'boolean';

interface FieldDefinition {
  key: keyof AppConfig;
  label: string;
  type: FieldType;
}

const FIELD_DEFINITIONS: FieldDefinition[] = [
  {key: 'profileName', label: 'Profile Name', type: 'text'},
  {key: 'simpleUrl', label: 'Simple URL', type: 'text'},
  {key: 'metadataRoot', label: 'Metadata Root', type: 'text'},
  {key: 'mirrorRoot', label: 'Mirror Root', type: 'text'},
  {key: 'concurrency', label: 'Concurrency', type: 'number'},
  {key: 'retry', label: 'Retry', type: 'number'},
  {key: 'timeoutMs', label: 'Timeout (ms)', type: 'number'},
  {key: 'userAgent', label: 'User Agent', type: 'text'},
  {key: 'downloadArtifacts', label: 'Download Artifacts', type: 'boolean'}
];

function formatValue(config: AppConfig, field: FieldDefinition): string {
  const value = config[field.key];
  if (field.type === 'boolean') {
    return value ? 'Yes' : 'No';
  }

  return String(value);
}

function commitFieldValue(config: AppConfig, field: FieldDefinition, input: string): AppConfig {
  if (field.type === 'number') {
    const parsed = Number(input.trim());
    return {
      ...config,
      [field.key]: Number.isFinite(parsed) ? parsed : config[field.key]
    };
  }

  if (field.type === 'boolean') {
    return {
      ...config,
      [field.key]: input.trim().toLowerCase() === 'true'
    };
  }

  return {
    ...config,
    [field.key]: input
  };
}

function appendLog(
  logs: string[],
  line: string
): string[] {
  return [...logs, line].slice(-14);
}

function InteractiveApp(): React.JSX.Element {
  const {exit} = useApp();
  const [config, setConfig] = useState<AppConfig>(defaultConfig());
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [editingKey, setEditingKey] = useState<keyof AppConfig | undefined>();
  const [draftValue, setDraftValue] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [running, setRunning] = useState(false);
  const [status, setStatus] = useState('Loading config...');
  const [logs, setLogs] = useState<string[]>([]);
  const [lastResult, setLastResult] = useState<SyncRunResult | undefined>();

  const selectedField = FIELD_DEFINITIONS[selectedIndex];
  const isEditing = Boolean(editingKey);

  useEffect(() => {
    void (async () => {
      const loadedConfig = await loadConfig();
      setConfig(loadedConfig);
      setLoading(false);
      setStatus(`Config loaded from ${DEFAULT_CONFIG_PATH}`);
    })();
  }, []);

  const saveCurrentConfig = async (): Promise<void> => {
    setSaving(true);
    const normalized = normalizeConfig(config);
    setConfig(normalized);
    await saveConfig(normalized);
    setStatus(`Config saved to ${DEFAULT_CONFIG_PATH}`);
    setLogs((current) => appendLog(current, `[config] saved ${DEFAULT_CONFIG_PATH}`));
    setSaving(false);
  };

  const startEditing = (): void => {
    if (!selectedField || selectedField.type === 'boolean') {
      return;
    }

    setEditingKey(selectedField.key);
    setDraftValue(String(config[selectedField.key]));
    setStatus(`Editing ${selectedField.label}`);
  };

  const toggleBooleanField = (): void => {
    if (!selectedField || selectedField.type !== 'boolean') {
      return;
    }

    setConfig((current) => ({
      ...current,
      [selectedField.key]: !current[selectedField.key]
    }));
    setStatus(`Updated ${selectedField.label}`);
  };

  const runCurrentConfig = async (): Promise<void> => {
    setRunning(true);
    setLastResult(undefined);
    setLogs((current) => appendLog(current, '[run] starting full sync'));

    try {
      const normalized = normalizeConfig(config);
      setConfig(normalized);
      await saveConfig(normalized);
      const result = await runSync({
        config: normalized,
        onEvent: (event: SyncEvent) => {
          setStatus(`${event.stage}: ${event.message}`);
          setLogs((current) => appendLog(current, `[${event.stage}] ${event.message}`));
        }
      });
      setLastResult(result);
      setStatus(`Run completed: snapshot ${result.snapshotId}`);
      setLogs((current) => appendLog(current, `[done] snapshot ${result.snapshotId}`));
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      setStatus(`Run failed: ${message}`);
      setLogs((current) => appendLog(current, `[error] ${message}`));
    } finally {
      setRunning(false);
    }
  };

  useInput((input, key) => {
    if (key.ctrl && input === 'c') {
      exit();
      return;
    }

    if (running) {
      if (input === 'q') {
        exit();
      }
      return;
    }

    if (isEditing) {
      if (key.escape) {
        setEditingKey(undefined);
        setDraftValue('');
        setStatus('Edit cancelled');
      }
      return;
    }

    if (key.upArrow) {
      setSelectedIndex((current) => (current - 1 + FIELD_DEFINITIONS.length) % FIELD_DEFINITIONS.length);
      return;
    }

    if (key.downArrow) {
      setSelectedIndex((current) => (current + 1) % FIELD_DEFINITIONS.length);
      return;
    }

    if (input === ' ') {
      toggleBooleanField();
      return;
    }

    if (key.return) {
      if (selectedField?.type === 'boolean') {
        toggleBooleanField();
      } else {
        startEditing();
      }
      return;
    }

    if (input === 'e') {
      startEditing();
      return;
    }

    if (input === 's') {
      void saveCurrentConfig();
      return;
    }

    if (input === 'r') {
      void runCurrentConfig();
      return;
    }

    if (input === 'q') {
      exit();
    }
  });

  const summaryLines = useMemo(() => {
    if (!lastResult) {
      return [];
    }

    return [
      `Snapshot: ${lastResult.snapshotId}`,
      `Packages: ${lastResult.packageCount}`,
      `Artifacts: ${lastResult.manifest.stats.artifactsTotal}`,
      `Plan entries: ${lastResult.plan.entries.length}`,
      `Added: ${lastResult.diff?.added.length ?? lastResult.plan.entries.length}`,
      `Changed: ${lastResult.diff?.changed.length ?? 0}`,
      `Downloaded: ${lastResult.downloadSummary?.downloaded ?? 0}`,
      `Failed: ${lastResult.downloadSummary?.failed.length ?? 0}`
    ];
  }, [lastResult]);

  return (
    <Box flexDirection="column" padding={1}>
      <Text color="cyan">mirror-sync</Text>
      <Text dimColor>Single-command TUI for PyPI mirror sync configuration and execution</Text>
      <Text dimColor>Config file: {DEFAULT_CONFIG_PATH}</Text>

      <Box marginTop={1} flexDirection="column">
        <Text color="yellow">Config</Text>
        {FIELD_DEFINITIONS.map((field, index) => {
          const selected = index === selectedIndex;
          const editing = editingKey === field.key;
          return (
            <Box key={String(field.key)}>
                {selected ? <Text color="green">&gt;</Text> : <Text> </Text>}
              <Text> {field.label}: </Text>
              {editing ? (
                <TextInput
                  value={draftValue}
                  onChange={setDraftValue}
                  onSubmit={(value) => {
                    setConfig((current) => commitFieldValue(current, field, value));
                    setEditingKey(undefined);
                    setDraftValue('');
                    setStatus(`Updated ${field.label}`);
                  }}
                />
              ) : (
                  selected ? <Text color="green">{formatValue(config, field)}</Text> : <Text>{formatValue(config, field)}</Text>
              )}
            </Box>
          );
        })}
      </Box>

      <Box marginTop={1} flexDirection="column">
        <Text color="yellow">Status</Text>
        <Text>{loading ? 'Loading...' : status}</Text>
        <Text dimColor>
          {running ? 'Running full sync...' : saving ? 'Saving config...' : 'Idle'}
        </Text>
      </Box>

      <Box marginTop={1} flexDirection="column">
        <Text color="yellow">Keys</Text>
        <Text>Up/Down select field | Enter edit/toggle | Space toggle boolean</Text>
        <Text>s save config | r run full sync | q quit | Esc cancel edit</Text>
      </Box>

      <Box marginTop={1} flexDirection="column">
        <Text color="yellow">Recent Events</Text>
        {logs.length === 0 ? <Text dimColor>No runs yet.</Text> : logs.map((line, index) => <Text key={`${index}-${line}`}>{line}</Text>)}
      </Box>

      <Box marginTop={1} flexDirection="column">
        <Text color="yellow">Last Result</Text>
        {summaryLines.length === 0 ? (
          <Text dimColor>No completed run yet.</Text>
        ) : (
          summaryLines.map((line, index) => <Text key={`${index}-${line}`}>{line}</Text>)
        )}
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
      </Box>
    );
  }

  return <InteractiveApp />;
}

render(<App />);
