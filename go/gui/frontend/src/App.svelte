<script lang="ts">
  import { onMount } from 'svelte';
  import { EventsOn } from '../wailsjs/runtime/runtime';
  import {
    LoadSettings,
    LoadTelegramConfig,
    SaveSettings,
    StartRun,
    StartSendImages,
    StartSendFiles,
    PauseRun,
    ResumeRun,
    StopRun,
    RunStatus,
    PickFile,
    PickDirectory
  } from '../wailsjs/go/main/App';

  type SettingsBundle = {
    settings: Record<string, any>;
    telegram: Record<string, any>;
    settings_path: string;
  };

  const defaultSettings = {
    config_path: '',
    chat_id: '',
    topic_id: null,
    watch_dir: '',
    queue_file: 'queue.jsonl',
    recursive: false,
    with_image: true,
    with_video: false,
    with_audio: false,
    with_all: false,
    include: [],
    exclude: [],
    zip_passwords: [],
    zip_pass_file: '',
    scan_interval_sec: 30,
    send_interval_sec: 30,
    settle_seconds: 5,
    group_size: 4,
    batch_delay_sec: 3,
    pause_every: 0,
    pause_seconds_sec: 0,
    notify_enabled: false,
    notify_interval_sec: 300,
    max_dimension: 2000,
    max_bytes: 5 * 1024 * 1024,
    png_start_level: 8
  };

  let bundle: SettingsBundle = {
    settings: { ...defaultSettings },
    telegram: { api_urls: [], tokens: [] },
    settings_path: ''
  };

  let apiURLs = '';
  let tokens = '';
  let includeGlobs = '';
  let excludeGlobs = '';
  let zipPasswords = '';
  let topicIdValue = '';

  const tabs = [
    { id: 'watch', label: 'Watch' },
    { id: 'send-images', label: 'Send Images' },
    { id: 'send-file', label: 'Send Files' },
    { id: 'send-video', label: 'Send Video' },
    { id: 'send-audio', label: 'Send Audio' }
  ] as const;

  type TabId = (typeof tabs)[number]['id'];
  let activeTab: TabId = 'watch';
  $: activeTabLabelText = tabs.find((tab) => tab.id === activeTab)?.label ?? 'Mode';

  let sendImageDir = '';
  let sendImageZip = '';
  let sendFilePath = '';
  let sendFileDir = '';
  let sendFileZip = '';
  let sendEnableZip = false;
  let sendGroupSize = 4;
  let sendStartIndex = 0;
  let sendEndIndex = 0;
  let sendBatchDelay = 3;

  let status = { running: false, paused: false };
  let progress = {
    status: 'idle',
    current_file: '',
    remaining_files: 0,
    total_files: 0,
    completed_files: 0,
    per_file_ms: 0,
    eta_ms: 0
  };
  let message = '';

  $: progressPercent =
    progress.total_files > 0
      ? Math.min(100, Math.round((progress.completed_files / progress.total_files) * 100))
      : 0;

  const splitLines = (value: string): string[] =>
    value
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean);

  const joinLines = (values: string[] = []): string => values.join('\n');

  const formatMs = (value: number): string => {
    if (!value || value < 0) return '0s';
    const seconds = Math.floor(value / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const remSeconds = seconds % 60;
    const remMinutes = minutes % 60;
    if (hours > 0) return `${hours}h ${remMinutes}m ${remSeconds}s`;
    if (minutes > 0) return `${minutes}m ${remSeconds}s`;
    return `${remSeconds}s`;
  };

  const normalizeNumbers = () => {
    const s = bundle.settings;
    s.scan_interval_sec = Number(s.scan_interval_sec) || 0;
    s.send_interval_sec = Number(s.send_interval_sec) || 0;
    s.settle_seconds = Number(s.settle_seconds) || 0;
    s.group_size = Number(s.group_size) || 0;
    s.batch_delay_sec = Number(s.batch_delay_sec) || 0;
    s.pause_every = Number(s.pause_every) || 0;
    s.pause_seconds_sec = Number(s.pause_seconds_sec) || 0;
    s.notify_interval_sec = Number(s.notify_interval_sec) || 0;
    s.max_dimension = Number(s.max_dimension) || 0;
    s.max_bytes = Number(s.max_bytes) || 0;
    s.png_start_level = Number(s.png_start_level) || 0;
  };

  const applyForm = () => {
    bundle.telegram.api_urls = splitLines(apiURLs);
    bundle.telegram.tokens = splitLines(tokens);
    bundle.settings.include = splitLines(includeGlobs);
    bundle.settings.exclude = splitLines(excludeGlobs);
    bundle.settings.zip_passwords = splitLines(zipPasswords);
    bundle.settings.topic_id = topicIdValue ? Number(topicIdValue) : null;
    normalizeNumbers();
  };

  const hydrateForm = () => {
    apiURLs = joinLines(bundle.telegram.api_urls);
    tokens = joinLines(bundle.telegram.tokens);
    includeGlobs = joinLines(bundle.settings.include);
    excludeGlobs = joinLines(bundle.settings.exclude);
    zipPasswords = joinLines(bundle.settings.zip_passwords);
    topicIdValue =
      bundle.settings.topic_id !== null && bundle.settings.topic_id !== undefined
        ? String(bundle.settings.topic_id)
        : '';
  };

  const dirname = (value: string): string => {
    if (!value) return '';
    const normalized = value.replace(/\\/g, '/');
    const index = normalized.lastIndexOf('/');
    if (index <= 0) return '';
    return normalized.slice(0, index);
  };

  const openFileDialog = async (title: string, current: string): Promise<string> => {
    message = '';
    try {
      const result = await PickFile(title, dirname(current));
      return result || '';
    } catch (err) {
      message = `Dialog failed: ${String(err)}`;
      return '';
    }
  };

  const openDirectoryDialog = async (title: string, current: string): Promise<string> => {
    message = '';
    try {
      const result = await PickDirectory(title, dirname(current));
      return result || '';
    } catch (err) {
      message = `Dialog failed: ${String(err)}`;
      return '';
    }
  };

  const load = async () => {
    message = '';
    try {
      bundle = await LoadSettings();
      bundle.settings = { ...defaultSettings, ...bundle.settings };
      hydrateForm();
      status = await RunStatus();
    } catch (err) {
      message = `Load failed: ${String(err)}`;
    }
  };

  const loadTelegramFromPath = async (path: string) => {
    if (!path) return;
    try {
      const cfg = await LoadTelegramConfig(path);
      bundle.telegram = cfg;
      hydrateForm();
    } catch (err) {
      message = `Load config failed: ${String(err)}`;
    }
  };

  const save = async () => {
    message = '';
    try {
      applyForm();
      await SaveSettings(bundle);
      message = 'Saved settings.';
    } catch (err) {
      message = `Save failed: ${String(err)}`;
    }
  };

  const startWatch = async () => {
    applyForm();
    await StartRun(bundle);
    status = await RunStatus();
  };

  const startSendImages = async () => {
    applyForm();
    await StartSendImages(bundle, {
      image_dir: sendImageDir,
      zip_file: sendImageZip,
      group_size: Number(sendGroupSize) || 0,
      start_index: Number(sendStartIndex) || 0,
      end_index: Number(sendEndIndex) || 0,
      batch_delay_sec: Number(sendBatchDelay) || 0,
      enable_zip: sendEnableZip
    });
    status = await RunStatus();
  };

  const startSendFiles = async (sendType: string) => {
    applyForm();
    await StartSendFiles(bundle, {
      send_type: sendType,
      file_path: sendFilePath,
      dir_path: sendFileDir,
      zip_file: sendFileZip,
      start_index: Number(sendStartIndex) || 0,
      end_index: Number(sendEndIndex) || 0,
      batch_delay_sec: Number(sendBatchDelay) || 0,
      enable_zip: sendEnableZip
    });
    status = await RunStatus();
  };

  const startAction = async () => {
    message = '';
    try {
      if (activeTab === 'watch') {
        await startWatch();
      } else if (activeTab === 'send-images') {
        await startSendImages();
      } else if (activeTab === 'send-file') {
        await startSendFiles('file');
      } else if (activeTab === 'send-video') {
        await startSendFiles('video');
      } else if (activeTab === 'send-audio') {
        await startSendFiles('audio');
      }
    } catch (err) {
      message = `Start failed: ${String(err)}`;
    }
  };

  const pause = async () => {
    message = '';
    try {
      await PauseRun();
      status = await RunStatus();
    } catch (err) {
      message = `Pause failed: ${String(err)}`;
    }
  };

  const resume = async () => {
    message = '';
    try {
      await ResumeRun();
      status = await RunStatus();
    } catch (err) {
      message = `Resume failed: ${String(err)}`;
    }
  };

  const stop = async () => {
    message = '';
    try {
      await StopRun();
      status = await RunStatus();
    } catch (err) {
      message = `Stop failed: ${String(err)}`;
    }
  };

  const pickConfigPath = async () => {
    const result = await openFileDialog('Select config file', bundle.settings.config_path);
    if (result) {
      bundle.settings.config_path = result;
      await loadTelegramFromPath(result);
    }
  };

  const pickWatchDir = async () => {
    const result = await openDirectoryDialog('Select watch directory', bundle.settings.watch_dir);
    if (result) bundle.settings.watch_dir = result;
  };

  const pickQueueFile = async () => {
    const result = await openFileDialog('Select queue file', bundle.settings.queue_file);
    if (result) bundle.settings.queue_file = result;
  };

  const pickZipPasswordFile = async () => {
    const result = await openFileDialog('Select zip password file', bundle.settings.zip_pass_file);
    if (result) bundle.settings.zip_pass_file = result;
  };

  const pickSendImageDir = async () => {
    const result = await openDirectoryDialog('Select image directory', sendImageDir);
    if (result) sendImageDir = result;
  };

  const pickSendImageZip = async () => {
    const result = await openFileDialog('Select image zip', sendImageZip);
    if (result) sendImageZip = result;
  };

  const pickSendFilePath = async () => {
    const result = await openFileDialog('Select file', sendFilePath);
    if (result) sendFilePath = result;
  };

  const pickSendFileDir = async () => {
    const result = await openDirectoryDialog('Select directory', sendFileDir);
    if (result) sendFileDir = result;
  };

  const pickSendFileZip = async () => {
    const result = await openFileDialog('Select zip file', sendFileZip);
    if (result) sendFileZip = result;
  };

  onMount(() => {
    load();
    EventsOn('progress', (data: any) => {
      progress = data ?? progress;
    });
    EventsOn('run-status', (data: any) => {
      status = data ?? status;
    });
    EventsOn('run-error', (data: any) => {
      message = String(data);
    });
  });
</script>

<main class="min-h-screen">
  <div class="mx-auto max-w-5xl px-4 py-8">
    <header class="mb-6">
      <p class="text-xs uppercase tracking-[0.4em] text-slate-400">Wails GUI</p>
      <h1 class="mt-2 text-3xl font-semibold text-slate-900">Telegram Upload Watcher</h1>
      <p class="mt-2 text-base text-slate-600">
        Configure Telegram targets, switch modes, and track progress with per-file timing and ETA.
      </p>
    </header>

    <div class="grid gap-4 lg:grid-cols-[1.25fr_0.9fr]">
      <fluent-card class="space-y-6">
        <div>
          <h2 class="text-xl font-semibold text-slate-900">Mode</h2>
          <p class="mt-1 text-sm text-slate-500">Active: {activeTabLabelText}</p>
        </div>

        <div class="flex flex-wrap gap-2" role="tablist">
          {#each tabs as tab}
            <fluent-button
              appearance={activeTab === tab.id ? 'accent' : 'outline'}
              on:click={() => (activeTab = tab.id)}
            >
              {tab.label}
            </fluent-button>
          {/each}
        </div>

        {#if activeTab === 'watch'}
          <div class="grid gap-4">
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Watch dir</label>
              <div class="mt-2 grid gap-2 lg:grid-cols-[1fr_auto] lg:items-end">
                <fluent-text-field
                  value={bundle.settings.watch_dir}
                  placeholder="/path/to/watch"
                  on:input={(event) => (bundle.settings.watch_dir = event.target.value)}
                />
                <fluent-button appearance="outline" on:click={pickWatchDir}>Browse</fluent-button>
              </div>
            </div>
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Queue file</label>
              <div class="mt-2 grid gap-2 lg:grid-cols-[1fr_auto] lg:items-end">
                <fluent-text-field
                  value={bundle.settings.queue_file}
                  placeholder="queue.jsonl"
                  on:input={(event) => (bundle.settings.queue_file = event.target.value)}
                />
                <fluent-button appearance="outline" on:click={pickQueueFile}>Browse</fluent-button>
              </div>
            </div>
          </div>

          <div class="grid gap-3 lg:grid-cols-3">
            <fluent-checkbox checked={bundle.settings.recursive} on:change={() => (bundle.settings.recursive = !bundle.settings.recursive)}>
              Recursive
            </fluent-checkbox>
            <fluent-checkbox checked={bundle.settings.with_image} on:change={() => (bundle.settings.with_image = !bundle.settings.with_image)}>
              Images
            </fluent-checkbox>
            <fluent-checkbox checked={bundle.settings.with_video} on:change={() => (bundle.settings.with_video = !bundle.settings.with_video)}>
              Video
            </fluent-checkbox>
            <fluent-checkbox checked={bundle.settings.with_audio} on:change={() => (bundle.settings.with_audio = !bundle.settings.with_audio)}>
              Audio
            </fluent-checkbox>
            <fluent-checkbox checked={bundle.settings.with_all} on:change={() => (bundle.settings.with_all = !bundle.settings.with_all)}>
              All files
            </fluent-checkbox>
            <fluent-checkbox checked={bundle.settings.notify_enabled} on:change={() => (bundle.settings.notify_enabled = !bundle.settings.notify_enabled)}>
              Notify
            </fluent-checkbox>
          </div>
        {:else if activeTab === 'send-images'}
          <div class="grid gap-4 lg:grid-cols-2">
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Image dir</label>
              <div class="mt-2 grid gap-2 lg:grid-cols-[1fr_auto] lg:items-end">
                <fluent-text-field
                  value={sendImageDir}
                  placeholder="/path/to/images"
                  on:input={(event) => (sendImageDir = event.target.value)}
                />
                <fluent-button appearance="outline" on:click={pickSendImageDir}>Browse</fluent-button>
              </div>
            </div>
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Zip file</label>
              <div class="mt-2 grid gap-2 lg:grid-cols-[1fr_auto] lg:items-end">
                <fluent-text-field
                  value={sendImageZip}
                  placeholder="/path/to/images.zip"
                  on:input={(event) => (sendImageZip = event.target.value)}
                />
                <fluent-button appearance="outline" on:click={pickSendImageZip}>Browse</fluent-button>
              </div>
            </div>
          </div>

          <div class="grid gap-4 lg:grid-cols-4">
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Group size</label>
              <fluent-text-field
                class="mt-2"
                type="number"
                value={sendGroupSize}
                on:input={(event) => (sendGroupSize = event.target.value)}
              />
            </div>
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Start index</label>
              <fluent-text-field
                class="mt-2"
                type="number"
                value={sendStartIndex}
                on:input={(event) => (sendStartIndex = event.target.value)}
              />
            </div>
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">End index</label>
              <fluent-text-field
                class="mt-2"
                type="number"
                value={sendEndIndex}
                on:input={(event) => (sendEndIndex = event.target.value)}
              />
            </div>
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Batch delay</label>
              <fluent-text-field
                class="mt-2"
                type="number"
                value={sendBatchDelay}
                on:input={(event) => (sendBatchDelay = event.target.value)}
              />
            </div>
          </div>

          <fluent-checkbox checked={sendEnableZip} on:change={() => (sendEnableZip = !sendEnableZip)}>
            Enable zip scanning
          </fluent-checkbox>
        {:else}
          <div class="grid gap-4 lg:grid-cols-2">
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">File path</label>
              <div class="mt-2 grid gap-2 lg:grid-cols-[1fr_auto] lg:items-end">
                <fluent-text-field
                  value={sendFilePath}
                  placeholder="/path/to/file"
                  on:input={(event) => (sendFilePath = event.target.value)}
                />
                <fluent-button appearance="outline" on:click={pickSendFilePath}>Browse</fluent-button>
              </div>
            </div>
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Directory</label>
              <div class="mt-2 grid gap-2 lg:grid-cols-[1fr_auto] lg:items-end">
                <fluent-text-field
                  value={sendFileDir}
                  placeholder="/path/to/dir"
                  on:input={(event) => (sendFileDir = event.target.value)}
                />
                <fluent-button appearance="outline" on:click={pickSendFileDir}>Browse</fluent-button>
              </div>
            </div>
          </div>
          <div class="grid gap-4 lg:grid-cols-2">
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Zip file</label>
              <div class="mt-2 grid gap-2 lg:grid-cols-[1fr_auto] lg:items-end">
                <fluent-text-field
                  value={sendFileZip}
                  placeholder="/path/to/archive.zip"
                  on:input={(event) => (sendFileZip = event.target.value)}
                />
                <fluent-button appearance="outline" on:click={pickSendFileZip}>Browse</fluent-button>
              </div>
            </div>
            <div class="flex items-end">
              <fluent-checkbox checked={sendEnableZip} on:change={() => (sendEnableZip = !sendEnableZip)}>
                Enable zip scanning
              </fluent-checkbox>
            </div>
          </div>

          <div class="grid gap-4 lg:grid-cols-4">
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Start index</label>
              <fluent-text-field
                class="mt-2"
                type="number"
                value={sendStartIndex}
                on:input={(event) => (sendStartIndex = event.target.value)}
              />
            </div>
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">End index</label>
              <fluent-text-field
                class="mt-2"
                type="number"
                value={sendEndIndex}
                on:input={(event) => (sendEndIndex = event.target.value)}
              />
            </div>
            <div>
              <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Batch delay</label>
              <fluent-text-field
                class="mt-2"
                type="number"
                value={sendBatchDelay}
                on:input={(event) => (sendBatchDelay = event.target.value)}
              />
            </div>
          </div>
        {/if}

        <details class="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">
          <summary class="cursor-pointer text-sm font-semibold text-slate-600">Advanced settings</summary>
          <div class="mt-4 grid gap-4">
            <div class="grid gap-4">
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Include globs</label>
                <fluent-text-area
                  class="mt-2"
                  rows="3"
                  value={includeGlobs}
                  placeholder="*.jpg\n*.png"
                  on:input={(event) => (includeGlobs = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Exclude globs</label>
                <fluent-text-area
                  class="mt-2"
                  rows="3"
                  value={excludeGlobs}
                  placeholder="*_thumb.*"
                  on:input={(event) => (excludeGlobs = event.target.value)}
                />
              </div>
            </div>

            <div class="grid gap-4">
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Zip passwords</label>
                <fluent-text-area
                  class="mt-2"
                  rows="3"
                  value={zipPasswords}
                  placeholder="password1\npassword2"
                  on:input={(event) => (zipPasswords = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Zip password file</label>
                <div class="mt-2 grid gap-2 lg:grid-cols-[1fr_auto] lg:items-end">
                  <fluent-text-field
                    value={bundle.settings.zip_pass_file}
                    placeholder="/path/to/passwords.txt"
                    on:input={(event) => (bundle.settings.zip_pass_file = event.target.value)}
                  />
                  <fluent-button appearance="outline" on:click={pickZipPasswordFile}>Browse</fluent-button>
                </div>
              </div>
            </div>

            <div class="grid gap-4 lg:grid-cols-2">
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Scan interval</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.scan_interval_sec}
                  on:input={(event) => (bundle.settings.scan_interval_sec = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Send interval</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.send_interval_sec}
                  on:input={(event) => (bundle.settings.send_interval_sec = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Settle seconds</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.settle_seconds}
                  on:input={(event) => (bundle.settings.settle_seconds = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Group size</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.group_size}
                  on:input={(event) => (bundle.settings.group_size = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Batch delay</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.batch_delay_sec}
                  on:input={(event) => (bundle.settings.batch_delay_sec = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Pause every</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.pause_every}
                  on:input={(event) => (bundle.settings.pause_every = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Pause seconds</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.pause_seconds_sec}
                  on:input={(event) => (bundle.settings.pause_seconds_sec = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Notify interval</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.notify_interval_sec}
                  on:input={(event) => (bundle.settings.notify_interval_sec = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Max dimension</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.max_dimension}
                  on:input={(event) => (bundle.settings.max_dimension = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Max bytes</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.max_bytes}
                  on:input={(event) => (bundle.settings.max_bytes = event.target.value)}
                />
              </div>
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">PNG level</label>
                <fluent-text-field
                  class="mt-2"
                  type="number"
                  value={bundle.settings.png_start_level}
                  on:input={(event) => (bundle.settings.png_start_level = event.target.value)}
                />
              </div>
            </div>
          </div>
        </details>
      </fluent-card>

      <div class="flex flex-col gap-4">
        <fluent-card>
          <details class="space-y-5">
            <summary class="flex cursor-pointer items-start justify-between gap-4">
              <div>
                <h2 class="text-xl font-semibold text-slate-900">Configuration</h2>
                <p class="mt-1 text-sm text-slate-500">Settings file: {bundle.settings_path || 'default'}</p>
              </div>
              <fluent-button
                appearance="accent"
                on:click|preventDefault|stopPropagation={save}
              >
                Save
              </fluent-button>
            </summary>

            <div class="grid gap-5">
              <div>
                <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Config path</label>
                <div class="mt-2 grid gap-2 lg:grid-cols-[1fr_auto] lg:items-end">
                  <fluent-text-field
                    value={bundle.settings.config_path}
                    placeholder="/path/to/config.ini"
                    on:input={(event) => (bundle.settings.config_path = event.target.value)}
                    on:change={() => loadTelegramFromPath(bundle.settings.config_path)}
                  />
                  <fluent-button appearance="outline" on:click={pickConfigPath}>Browse</fluent-button>
                </div>
              </div>

              <div class="grid gap-4">
                <div>
                  <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">API URLs</label>
                  <fluent-text-area
                    class="mt-2"
                    rows="3"
                    value={apiURLs}
                    placeholder="https://api.telegram.org"
                    on:input={(event) => (apiURLs = event.target.value)}
                  />
                </div>
                <div>
                  <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Bot tokens</label>
                  <fluent-text-area
                    class="mt-2"
                    rows="3"
                    value={tokens}
                    placeholder="123456:ABCDEF"
                    on:input={(event) => (tokens = event.target.value)}
                  />
                </div>
              </div>

              <div class="grid gap-4">
                <div>
                  <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Chat ID</label>
                  <fluent-text-field
                    class="mt-2"
                    value={bundle.settings.chat_id}
                    placeholder="-1001234567890"
                    on:input={(event) => (bundle.settings.chat_id = event.target.value)}
                  />
                </div>
                <div>
                  <label class="text-sm font-semibold uppercase tracking-wide text-slate-500">Topic ID</label>
                  <fluent-text-field
                    class="mt-2"
                    value={topicIdValue}
                    placeholder="Optional"
                    on:input={(event) => (topicIdValue = event.target.value)}
                  />
                </div>
              </div>
            </div>
          </details>
        </fluent-card>

        <fluent-card>
          <h2 class="text-xl font-semibold text-slate-900">Run controls</h2>
          <p class="mt-1 text-sm text-slate-500">Active mode: {activeTabLabelText}</p>
          <div class="mt-4 flex flex-wrap gap-3">
            <fluent-button appearance="accent" on:click={startAction} disabled={status.running}>
              {activeTab === 'watch' ? 'Start watch' : 'Start send'}
            </fluent-button>
            <fluent-button appearance="outline" on:click={pause} disabled={!status.running || status.paused}>
              Pause
            </fluent-button>
            <fluent-button appearance="outline" on:click={resume} disabled={!status.paused}>
              Continue
            </fluent-button>
            <fluent-button appearance="stealth" on:click={stop} disabled={!status.running}>
              Stop
            </fluent-button>
          </div>
          <div class="mt-4 rounded-2xl bg-slate-100 px-4 py-3 text-base text-slate-700">
            {#if status.running}
              {status.paused ? 'Paused' : 'Running'}
            {:else}
              Idle
            {/if}
          </div>
          {#if message}
            <p class="mt-3 text-sm text-amber-600">{message}</p>
          {/if}
        </fluent-card>
      </div>
    </div>

    <div class="mt-6">
      <fluent-card>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 class="text-xl font-semibold text-slate-900">Progress</h2>
            <p class="mt-1 text-sm text-slate-500">
              {progress.completed_files}/{progress.total_files || 0} completed
            </p>
          </div>
          <div class="text-sm text-slate-500">Remaining: {progress.remaining_files}</div>
        </div>
        <div class="mt-4">
          <progress class="progress-bar" value={progressPercent} max="100"></progress>
          <div class="mt-2 flex items-center justify-between text-sm text-slate-500">
            <span>{progressPercent}%</span>
            <span>ETA {formatMs(progress.eta_ms)}</span>
          </div>
        </div>
        <div class="mt-4 grid gap-3 lg:grid-cols-[2fr_1fr_1fr]">
          <div class="rounded-2xl bg-slate-100 px-4 py-3">
            <p class="text-xs uppercase tracking-wide text-slate-500">Current file</p>
            <p class="mt-1 text-base font-medium">{progress.current_file || '—'}</p>
          </div>
          <div class="rounded-2xl bg-slate-100 px-4 py-3">
            <p class="text-xs uppercase tracking-wide text-slate-500">Per-file time</p>
            <p class="mt-1 text-base font-medium">{formatMs(progress.per_file_ms)}</p>
          </div>
          <div class="rounded-2xl bg-slate-100 px-4 py-3">
            <p class="text-xs uppercase tracking-wide text-slate-500">Status</p>
            <p class="mt-1 text-base font-medium">{progress.status}</p>
          </div>
        </div>
      </fluent-card>
    </div>
  </div>
</main>
