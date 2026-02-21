// Captain's Log — Browser client
// Handles recording, transcription, stardates, history, settings, and AI forwarding.

(function () {
    'use strict';

    // --- DOM refs ---
    const recordBtn = document.getElementById('recordBtn');
    // Onboarding: pulse the record button until first use
    if (!localStorage.getItem('captainslog_used')) {
        recordBtn.classList.add('onboarding');
    }
    const micIcon = document.getElementById('micIcon');
    const stopIcon = document.getElementById('stopIcon');
    const timer = document.getElementById('timer');
    const waveform = document.getElementById('waveform');
    const uploadZone = document.getElementById('uploadZone');
    const fileInput = document.getElementById('fileInput');
    const browseBtn = document.getElementById('browseBtn');
    const transcriptionText = document.getElementById('transcriptionText');
    const placeholder = document.getElementById('placeholder');
    const processing = document.getElementById('processing');
    const processingLabel = document.getElementById('processingLabel');
    const copyBtn = document.getElementById('copyBtn');
    const saveBtn = document.getElementById('saveBtn');
    const goToTextBtn = document.getElementById('goToTextBtn');
    const goToAudioBtn = document.getElementById('goToAudioBtn');
    const llmBtn = document.getElementById('llmBtn');
    const backendStatus = document.getElementById('backendStatus');
    const stardateDisplay = document.getElementById('stardateDisplay');
    const settingsBtn = document.getElementById('settingsBtn');
    const settingsModal = document.getElementById('settingsModal');
    const closeSettings = document.getElementById('closeSettings');
    const saveSettings = document.getElementById('saveSettings');
    const resetSettings = document.getElementById('resetSettings');
    const audioSourceSelect = document.getElementById('audioSource');
    const refreshSourcesBtn = document.getElementById('refreshSources');

    // --- Audio device enumeration ---
    async function enumerateAudioDevices() {
        try {
            // Need at least one getUserMedia call to get labels
            await navigator.mediaDevices.getUserMedia({ audio: true }).then(s => s.getTracks().forEach(t => t.stop()));
            const devices = await navigator.mediaDevices.enumerateDevices();
            const audioInputs = devices.filter(d => d.kind === 'audioinput');
            const saved = localStorage.getItem('captainslog_audio_source');
            audioSourceSelect.innerHTML = '';

            // Default option
            const defOpt = document.createElement('option');
            defOpt.value = 'default';
            defOpt.textContent = 'Default Microphone';
            audioSourceSelect.appendChild(defOpt);

            audioInputs.forEach(device => {
                if (device.deviceId === 'default' || device.deviceId === 'communications') return;
                const opt = document.createElement('option');
                opt.value = device.deviceId;
                let label = device.label || `Audio Input ${device.deviceId.substring(0, 8)}`;
                // Friendly labels for common devices
                if (/monitor/i.test(label)) label = '🔊 ' + label;
                else if (/r.decaster/i.test(label)) label = '🎛️ ' + label;
                else if (/obs/i.test(label)) label = '📺 ' + label;
                else if (/virtual/i.test(label)) label = '🔗 ' + label;
                else label = '🎙️ ' + label;
                opt.textContent = label;
                audioSourceSelect.appendChild(opt);
            });

            if (saved && audioSourceSelect.querySelector(`option[value="${saved}"]`)) {
                audioSourceSelect.value = saved;
            }
        } catch (err) {
            console.warn('Could not enumerate audio devices:', err);
        }
    }

    audioSourceSelect.addEventListener('change', () => {
        localStorage.setItem('captainslog_audio_source', audioSourceSelect.value);
    });

    refreshSourcesBtn.addEventListener('click', () => enumerateAudioDevices());
    enumerateAudioDevices();

    // --- Settings defaults ---
    const defaults = {
        vault_dir: '',
        download_dir: '',
        language: 'en',
        model: 'large-v3',
        auto_save: false,
        auto_copy: true,
        prompt: '',
        vad_filter: false,
        diarize: false,
        show_stardates: true,
        date_format: '2006-01-02',
        whisper_url: '',
        llm_url: '',
        llm_model: '',
        enable_llm: false,
        history_limit: 5
    };

    let settings = { ...defaults };

    // --- State ---
    let mediaRecorder = null;
    let audioChunks = [];
    let isRecording = false;
    let timerInterval = null;
    let startTime = 0;
    let streamingWs = null;
    let streamingNode = null;
    let audioContext = null;
    let analyser = null;
    let animationId = null;
    let currentTranscription = '';
    let currentSegments = [];

    // --- Transcription history (Clipy-inspired log archive) ---
    let logHistory = JSON.parse(localStorage.getItem('captainslog_history') || '[]');

    // --- Init ---
    loadSettings();
    updateHeaderTime();
    setInterval(updateHeaderTime, 10000);
    checkHealth();
    setInterval(checkHealth, 30000);

    // --- Header time (stardate or normal clock) ---
    function updateHeaderTime() {
        if (settings.show_stardates !== false) {
            fetch('/api/stardate').then(r => r.json()).then(data => {
                stardateDisplay.textContent = 'Stardate ' + data.stardate;
                stardateDisplay.title = data.earth;
            }).catch(() => {
                const now = new Date();
                const year = now.getFullYear();
                const dayOfYear = Math.floor((now - new Date(year, 0, 0)) / 86400000);
                const daysInYear = (year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)) ? 366 : 365;
                const sd = (100 * (year - 2323) + (dayOfYear / daysInYear) * 1000).toFixed(1);
                stardateDisplay.textContent = 'Stardate ' + sd;
            });
        } else {
            const now = new Date();
            stardateDisplay.textContent = now.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' }) + ' · ' + now.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
            stardateDisplay.title = now.toISOString();
        }
    }

    // --- Settings ---
    function loadSettings() {
        const stored = localStorage.getItem('captainslog_prefs');
        if (stored) {
            try { Object.assign(settings, JSON.parse(stored)); } catch (e) { console.warn('Failed to parse stored settings:', e); }
        }
        applySettings();
        fetch('/api/settings').then(r => r.json()).then(data => {
            Object.assign(settings, data);
            applySettings();
        }).catch((e) => { console.warn('Could not load settings from server:', e); });
    }

    function applySettings() {
        el('settLanguage').value = settings.language || 'en';
        el('settVaultDir').value = settings.vault_dir || '';
        el('settDownloadDir').value = settings.download_dir || '';
        el('settModel').value = settings.model || 'large-v3';
        el('settAutoCopy').checked = settings.auto_copy !== false;
        el('settAutoSave').checked = !!settings.auto_save;
        el('settPrompt').value = settings.prompt || '';
        el('settVAD').checked = !!settings.vad_filter;
        el('settDiarize').checked = !!settings.diarize;
        el('settStardates').checked = settings.show_stardates !== false;
        el('settDateFormat').value = settings.date_format || '2006-01-02';
        el('settFileTitle').value = settings.file_title || 'Dictation';
        el('settWhisperURL').value = settings.whisper_url || '';
        el('settLLMURL').value = settings.llm_url || '';
        el('settEnableLLM').checked = !!settings.enable_llm;
        el('settAccessLog').checked = !!settings.access_log;
        el('settTimeFormat').value = settings.time_format || 'system';

        // Advanced transcription parameters
        el('settWordTimestamps').checked = !!settings.word_timestamps;
        el('settConditionPrev').checked = settings.condition_on_previous_text !== false;
        el('settBeamSize').value = settings.beam_size ?? 5;
        el('settTemperature').value = settings.temperature ?? 0;

        // Translate mode toggle (in record controls, not settings)
        const translateEl = el('translateMode');
        if (translateEl) translateEl.checked = !!settings.translate_mode;

        // Show recordings dir (read-only)
        const recDir = (settings.config_dir || '~/.config/captainslog') + '/recordings';
        el('settRecordingsDir').value = recDir;
        el('settHistoryLimit').value = settings.history_limit ?? 5;
        el('settPort').value = window.location.port || '8090';
        el('settEnableTLS').checked = settings.enable_tls || false;
        el('settExportFormat').value = settings.default_export_format || 'txt';
        el('settExportMode').value = settings.export_mode || 'rich';
        el('settTranscriptDir').value = settings.transcript_dir || '';
        el('settTranslateDir').value = settings.translate_dir || '';
    }

    function saveSettingsToServer() {
        settings.vault_dir = el('settVaultDir').value.trim();
        settings.download_dir = el('settDownloadDir').value.trim();
        settings.language = el('settLanguage').value;
        settings.model = el('settModel').value;
        settings.auto_copy = el('settAutoCopy').checked;
        settings.auto_save = el('settAutoSave').checked;
        settings.prompt = el('settPrompt').value.trim();
        settings.vad_filter = el('settVAD').checked;
        settings.diarize = el('settDiarize').checked;
        settings.show_stardates = el('settStardates').checked;
        settings.date_format = el('settDateFormat').value;
        settings.file_title = el('settFileTitle').value.trim() || 'Dictation';
        settings.whisper_url = el('settWhisperURL').value.trim();
        settings.llm_url = el('settLLMURL').value.trim();
        settings.llm_model = el('settLLMModel')?.value || '';
        settings.enable_llm = el('settEnableLLM').checked;
        settings.access_log = el('settAccessLog').checked;

        // Show/hide LLM button
        if (llmBtn) llmBtn.style.display = settings.enable_llm ? '' : 'none';
        settings.time_format = el('settTimeFormat').value;
        settings.history_limit = parseInt(el('settHistoryLimit').value) || 5;
        settings.enable_tls = el('settEnableTLS').checked;
        settings.default_export_format = el('settExportFormat').value;

        // Advanced transcription parameters
        settings.word_timestamps = el('settWordTimestamps').checked;
        settings.condition_on_previous_text = el('settConditionPrev').checked;
        settings.beam_size = parseInt(el('settBeamSize').value) || 5;
        settings.temperature = parseFloat(el('settTemperature').value) || 0;

        // Export mode and auto-export directory
        settings.export_mode = el('settExportMode').value || 'rich';
        settings.transcript_dir = el('settTranscriptDir').value.trim();
        settings.translate_dir = el('settTranslateDir').value.trim();

        // Auto-switch default format if SRT/VTT selected but now in Pure mode
        if (settings.export_mode === 'pure' && (settings.default_export_format === 'srt' || settings.default_export_format === 'vtt')) {
            settings.default_export_format = 'txt';
            el('settExportFormat').value = 'txt';
        }


        updateHeaderTime();
        localStorage.setItem('captainslog_prefs', JSON.stringify(settings));
        fetch('/api/settings', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(settings)
        }).then(() => {
            flashButton(saveSettings, 'Saved!', 'success');
        }).catch(err => {
            console.error('Settings save failed:', err);
        });
    }

    function el(id) { return document.getElementById(id); }


    // --- LLM model refresh ---
    function fetchLLMModels() {
        fetch('/api/models').then(r => r.json()).then(data => {
            const select = el('settLLMModel');
            if (!select) return;
            const current = select.value || settings.llm_model || '';
            select.innerHTML = '';
            const models = data.llm || [];
            if (models.length === 0) {
                const opt = document.createElement('option');
                opt.value = '';
                opt.textContent = 'No models found';
                select.appendChild(opt);
            }
            models.forEach(m => {
                const opt = document.createElement('option');
                opt.value = m.id;
                opt.textContent = m.name;
                select.appendChild(opt);
            });
            if (current) select.value = current;
        }).catch(() => {
            const select = el('settLLMModel');
            if (select) select.innerHTML = '<option value="">LLM unreachable</option>';
        });
    }
    const refreshBtn = document.getElementById('refreshLLMModels');
    if (refreshBtn) refreshBtn.addEventListener('click', fetchLLMModels);

    // --- Settings modal ---
    settingsBtn.addEventListener('click', () => { applySettings(); fetchModels(); fetchLLMModels(); settingsModal.classList.remove('hidden'); });
    closeSettings.addEventListener('click', () => settingsModal.classList.add('hidden'));
    settingsModal.addEventListener('click', (e) => {
        if (e.target === settingsModal) settingsModal.classList.add('hidden');
        // Open directory buttons
        const openBtn = e.target.closest('[data-open-dir]');
        if (openBtn) {
            e.preventDefault();
            e.stopPropagation();
            const inputId = openBtn.dataset.openDir;
            const dir = el(inputId)?.value;
            if (dir) {
                fetch('/api/open', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ path: dir })
                });
            }
        }
    });
    saveSettings.addEventListener('click', () => { saveSettingsToServer(); setTimeout(() => settingsModal.classList.add('hidden'), 600); });
    resetSettings.addEventListener('click', () => { settings = { ...defaults }; applySettings(); });

    // --- Health ---
    async function checkHealth() {
        try {
            const res = await fetch('/healthz');
            const data = await res.json();
            backendStatus.className = 'status-dot ' + (data.whisper === 'connected' ? 'connected' : 'error');
            backendStatus.title = `Whisper: ${data.whisper} | Vault: ${data.vault ? 'on' : 'off'} | Stardate: ${data.stardate}`;
        } catch {
            backendStatus.className = 'status-dot error';
            backendStatus.title = 'Backend unreachable';
        }
    }

    // --- Model discovery ---
    function fetchModels() {
        fetch('/api/models').then(r => r.json()).then(data => {
            const modelSelect = el('settModel');
            if (data.whisper && data.whisper.length > 0) {
                const currentVal = modelSelect.value;
                modelSelect.innerHTML = '';
                data.whisper.forEach(m => {
                    const opt = document.createElement('option');
                    opt.value = m.id;
                    opt.textContent = m.name;
                    modelSelect.appendChild(opt);
                });
                modelSelect.value = currentVal || settings.model || 'large-v3';
            }
        }).catch(() => { }); // Use hardcoded fallback in HTML
    }

    // --- Recording ---
    recordBtn.addEventListener('click', toggleRecording);

    async function toggleRecording() {
        if (isRecording) stopRecording();
        else await startRecording();
    }

    async function startRecording() {
        // Remove onboarding pulse on first ever record
        recordBtn.classList.remove('onboarding');
        localStorage.setItem('captainslog_used', '1');
        try {
            const deviceId = audioSourceSelect.value;
            const audioConstraints = {
                channelCount: 1,
                sampleRate: 16000,
                echoCancellation: deviceId === 'default',
                noiseSuppression: deviceId === 'default'
            };
            if (deviceId && deviceId !== 'default') {
                audioConstraints.deviceId = { exact: deviceId };
            }
            const stream = await navigator.mediaDevices.getUserMedia({ audio: audioConstraints });
            audioContext = new (window.AudioContext || window.webkitAudioContext)();
            const source = audioContext.createMediaStreamSource(stream);
            analyser = audioContext.createAnalyser();
            analyser.fftSize = 256;
            source.connect(analyser);
            drawWaveform();
            mediaRecorder = new MediaRecorder(stream, {
                mimeType: MediaRecorder.isTypeSupported('audio/webm;codecs=opus') ? 'audio/webm;codecs=opus' : 'audio/webm'
            });
            audioChunks = [];
            mediaRecorder.ondataavailable = (e) => { if (e.data.size > 0) audioChunks.push(e.data); };
            mediaRecorder.onstop = () => {
                stream.getTracks().forEach(t => t.stop());
                const blob = new Blob(audioChunks, { type: mediaRecorder.mimeType });
                transcribeAudio(blob);
            };
            mediaRecorder.start(250);
            isRecording = true;
            recordBtn.classList.add('recording');
            micIcon.classList.add('hidden');
            stopIcon.classList.remove('hidden');
            startTime = Date.now();
            timerInterval = setInterval(updateTimer, 100);

            // Focus on recording area and hide placeholder
            recordBtn.scrollIntoView({ behavior: 'smooth', block: 'center' });
            document.getElementById('placeholder').style.display = 'none';

            // Start streaming if configured
            if (settings.stream_url) startStreaming(stream);
        } catch (err) {
            console.error('Microphone access denied:', err);
            const isHTTPIssue = location.protocol === 'http:' && location.hostname !== 'localhost' && location.hostname !== '127.0.0.1';
            const platform = navigator.platform || '';
            let msg = '⚠️ Microphone access required\n\n';

            if (isHTTPIssue) {
                msg += 'Chrome blocks microphone on non-HTTPS sites.\n\nFix:\n';
                msg += '1. Use http://localhost:8090\n';
                msg += '2. Set CAPTAINSLOG_ENABLE_TLS=true\n';
                msg += '3. chrome://flags → Insecure origins treated as secure';
            } else if (err.name === 'NotAllowedError') {
                msg += 'Your browser or OS blocked microphone access.\n\n';
                if (platform.includes('Mac')) {
                    msg += 'macOS: System Settings → Privacy & Security → Microphone → enable your browser';
                } else if (platform.includes('Win')) {
                    msg += 'Windows: Settings → Privacy → Microphone → allow browser access';
                } else {
                    msg += 'Linux: Check your browser permissions and PipeWire/PulseAudio settings.\n';
                    msg += 'Try: Settings → Site Settings → Microphone → Allow for this site';
                }
            } else if (err.name === 'NotFoundError') {
                msg += 'No microphone detected. Check your audio input device is connected and enabled.';
            } else {
                msg += 'Error: ' + err.message + '\n\nPlease check your microphone and browser settings.';
            }
            alert(msg);
        }
    }

    function stopRecording() {
        if (mediaRecorder && mediaRecorder.state !== 'inactive') mediaRecorder.stop();
        stopStreaming();
        isRecording = false;
        recordBtn.classList.remove('recording');
        micIcon.classList.remove('hidden');
        stopIcon.classList.add('hidden');
        clearInterval(timerInterval);
        if (animationId) cancelAnimationFrame(animationId);
        if (audioContext) audioContext.close();
        const ctx = waveform.getContext('2d');
        ctx.clearRect(0, 0, waveform.width, waveform.height);
    }

    // --- Live streaming transcription ---
    function startStreaming(mediaStream) {
        if (!settings.stream_url) return;
        try {
            streamingWs = new WebSocket(settings.stream_url);
            streamingWs.binaryType = 'arraybuffer';

            // Show LIVE badge
            const badge = document.querySelector('.connection-badge');
            if (badge) badge.dataset.streaming = 'true';

            // Update Latest card to show live text
            placeholder.classList.add('hidden');
            transcriptionText.textContent = '';
            transcriptionText.classList.remove('hidden');

            streamingWs.onmessage = (event) => {
                try {
                    const data = JSON.parse(event.data);
                    // Support common streaming response formats
                    const text = data.text || data.partial || data.segment?.text || '';
                    if (text) {
                        transcriptionText.textContent = text;
                    }
                } catch {
                    // Non-JSON message — treat as plain text
                    if (event.data && typeof event.data === 'string') {
                        transcriptionText.textContent = event.data;
                    }
                }
            };

            streamingWs.onerror = () => {
                console.warn('Streaming WebSocket error — falling back to post-recording transcription');
                stopStreaming();
            };

            streamingWs.onclose = () => {
                const b = document.querySelector('.connection-badge');
                if (b) delete b.dataset.streaming;
            };

            // Capture PCM audio via ScriptProcessorNode (16kHz, mono, Float32)
            const streamCtx = audioContext;
            if (streamCtx) {
                const source = streamCtx.createMediaStreamSource(mediaStream);
                const bufferSize = 4096;
                streamingNode = streamCtx.createScriptProcessor(bufferSize, 1, 1);
                const targetRate = 16000;
                const sourceRate = streamCtx.sampleRate;

                streamingNode.onaudioprocess = (e) => {
                    if (!streamingWs || streamingWs.readyState !== WebSocket.OPEN) return;
                    const input = e.inputBuffer.getChannelData(0);
                    // Downsample to 16kHz
                    const ratio = sourceRate / targetRate;
                    const length = Math.ceil(input.length / ratio);
                    const output = new Float32Array(length);
                    for (let i = 0; i < length; i++) {
                        output[i] = input[Math.round(i * ratio)] || 0;
                    }
                    streamingWs.send(output.buffer);
                };
                source.connect(streamingNode);
                streamingNode.connect(streamCtx.destination);
            }
        } catch (err) {
            console.warn('Failed to start streaming:', err);
        }
    }

    function stopStreaming() {
        if (streamingNode) {
            try { streamingNode.disconnect(); } catch { }
            streamingNode = null;
        }
        if (streamingWs) {
            try { streamingWs.close(); } catch { }
            streamingWs = null;
        }
        const badge = document.querySelector('.connection-badge');
        if (badge) delete badge.dataset.streaming;
    }

    function updateTimer() {
        const elapsed = Math.floor((Date.now() - startTime) / 1000);
        timer.textContent = `${String(Math.floor(elapsed / 60)).padStart(2, '0')}:${String(elapsed % 60).padStart(2, '0')}`;
    }

    // --- Waveform (frequency bars) ---
    function drawWaveform() {
        if (!analyser) return;

        // Sync canvas internal resolution with its display size
        const rect = waveform.getBoundingClientRect();
        const dpr = window.devicePixelRatio || 1;
        waveform.width = Math.round(rect.width * dpr);
        waveform.height = Math.round(rect.height * dpr);

        const ctx = waveform.getContext('2d');
        ctx.scale(dpr, dpr);

        const bufferLength = analyser.frequencyBinCount;
        const dataArray = new Uint8Array(bufferLength);
        const width = rect.width;
        const height = rect.height;
        const barCount = 64;
        const barWidth = (width / barCount) - 1;
        const gradient = ctx.createLinearGradient(0, height, 0, 0);
        gradient.addColorStop(0, '#c49a6c');
        gradient.addColorStop(0.6, '#d4a87a');
        gradient.addColorStop(1, '#fac863');
        function draw() {
            animationId = requestAnimationFrame(draw);
            analyser.getByteFrequencyData(dataArray);
            ctx.fillStyle = 'rgba(10, 14, 26, 0.4)';
            ctx.fillRect(0, 0, width, height);
            const step = Math.max(1, Math.floor(bufferLength / barCount));
            for (let i = 0; i < barCount; i++) {
                let sum = 0;
                for (let j = 0; j < step; j++) {
                    sum += dataArray[i * step + j] || 0;
                }
                const avg = sum / step;
                const barHeight = (avg / 255) * height * 0.9;
                const x = i * (barWidth + 1);
                ctx.fillStyle = gradient;
                ctx.fillRect(x, height - barHeight, barWidth, barHeight);
            }
        }
        draw();
    }

    // --- File upload (batch queue) ---
    browseBtn.addEventListener('click', (e) => { e.preventDefault(); fileInput.click(); });
    fileInput.addEventListener('change', (e) => { if (e.target.files.length) processFileQueue([...e.target.files]); });
    uploadZone.addEventListener('click', () => fileInput.click());
    uploadZone.addEventListener('dragover', (e) => { e.preventDefault(); uploadZone.classList.add('dragover'); });
    uploadZone.addEventListener('dragleave', () => uploadZone.classList.remove('dragover'));
    uploadZone.addEventListener('drop', (e) => {
        e.preventDefault();
        uploadZone.classList.remove('dragover');
        const files = [...e.dataTransfer.files].filter(f =>
            f.type.startsWith('audio/') || f.type.startsWith('video/') ||
            /\.(wav|mp3|mp4|m4a|ogg|flac|webm)$/i.test(f.name)
        );
        if (files.length) processFileQueue(files);
    });

    async function processFileQueue(files) {
        const total = files.length;
        for (let i = 0; i < total; i++) {
            if (total > 1) {
                processingLabel.textContent = `Processing ${i + 1} of ${total}: ${files[i].name}`;
            }
            await transcribeAudio(files[i]);
        }
        if (total > 1) processingLabel.textContent = `Done — ${total} files processed`;
    }

    // --- Transcription ---
    async function transcribeAudio(audioBlob) {
        // Read translate toggle early — needed for processing message and endpoint selection
        const translateMode = el('translateMode')?.checked || false;
        const processingMsg = translateMode ? 'Translating → English…' : 'Transcribing…';
        showProcessing(true, processingMsg);
        const formData = new FormData();
        formData.append('file', audioBlob, 'recording.webm');
        formData.append('response_format', 'verbose_json');
        const lang = settings.language || 'en';
        if (lang && lang !== 'und') formData.append('language', lang);
        if (settings.prompt) formData.append('prompt', settings.prompt);
        if (settings.vad_filter) formData.append('vad_filter', 'true');
        if (settings.diarize) formData.append('diarize', 'true');

        // Advanced parameters (feature parity with faster-whisper)
        if (settings.word_timestamps) formData.append('word_timestamps', 'true');
        if (settings.beam_size && settings.beam_size !== 5) formData.append('beam_size', String(settings.beam_size));
        if (settings.temperature && settings.temperature > 0) formData.append('temperature', String(settings.temperature));
        if (settings.condition_on_previous_text === false) formData.append('condition_on_previous_text', 'false');

        // Translate mode: use translation endpoint instead of transcription
        const endpoint = translateMode ? '/v1/audio/translations' : '/v1/audio/transcriptions';

        try {
            console.log(`Sending audio to ${endpoint}${translateMode ? ' (translate mode)' : ''}`);
            const res = await fetch(endpoint, { method: 'POST', body: formData });
            if (!res.ok) {
                let detail = '';
                try {
                    const errBody = await res.text();
                    const errJson = JSON.parse(errBody);
                    detail = errJson.detail || errJson.error || errJson.message || errBody;
                } catch {
                    // JSON parse failed — errBody was already consumed above, don't re-read
                    // detail stays empty, which is fine — the status hint covers it
                }
                const statusMessages = {
                    400: 'Bad request — the audio file may be corrupt, empty, or in an unsupported format.',
                    401: 'Unauthorized — check your auth token.',
                    404: 'Endpoint not found — the Whisper backend may not support translation.',
                    413: 'File too large — try a shorter recording or smaller file.',
                    415: 'Unsupported format — try WAV, MP3, or FLAC.',
                    422: 'Processing error — the backend could not process this audio.',
                    429: 'Rate limited — too many requests. Wait a moment.',
                    500: 'Backend error — the Whisper server encountered an internal error.',
                    502: 'Backend unreachable — is faster-whisper running?',
                    503: 'Backend unavailable — the Whisper server may be starting up.',
                };
                const hint = statusMessages[res.status] || `HTTP ${res.status}`;
                throw new Error(`${hint}${detail ? '\n\nBackend: ' + detail : ''}`);
            }
            let data;
            try {
                data = await res.json();
            } catch (parseErr) {
                // Backend returned non-JSON (common with translation endpoints)
                const rawText = await res.text().catch(() => '');
                console.error('Failed to parse response as JSON:', parseErr, 'Raw:', rawText);
                throw new Error(`Backend returned invalid JSON. The Whisper server may not support this endpoint.\n\nRaw response: ${rawText.slice(0, 200)}`);
            }
            let text = data.text || '';

            // Handle verbose_json with segments (word-level timestamps / speaker labels)
            if (data.segments && data.segments.length > 0) {
                currentSegments = data.segments;
                const formatted = data.segments.map(seg => {
                    let line = '';
                    // Speaker diarization label
                    if (seg.speaker !== undefined && seg.speaker !== null) {
                        const speakerClass = 'speaker-' + (seg.speaker % 4);
                        line += `<span class="speaker-label ${speakerClass}">SPEAKER ${seg.speaker + 1}</span>`;
                    }
                    // Timestamp
                    const start = formatTimestamp(seg.start);
                    line += `<span class="log-stardate">[${start}]</span> `;
                    line += escapeHTML(seg.text);
                    return line;
                }).join('\n');
                appendTranscription(formatted, true);
                text = data.segments.map(s => s.text).join(' ').trim();
                // Override — appendTranscription sets currentTranscription from
                // textContent which includes [00:00] timestamps. Copy must be pure text.
                currentTranscription = text;
            } else if (text.trim()) {
                currentSegments = [];
                appendTranscription(text.trim(), false);
            } else {
                currentSegments = [];
                appendTranscription('(No speech detected)', false);
            }

            if (text.trim()) {
                // Save recording to server
                let recordingFile = null;
                try {
                    const recForm = new FormData();
                    recForm.append('file', audioBlob, 'recording.webm');
                    const recRes = await fetch('/api/recordings', { method: 'POST', body: recForm });
                    if (recRes.ok) {
                        const recData = await recRes.json();
                        recordingFile = recData.filename;
                    }
                } catch (e) { console.warn('Recording save failed (non-critical):', e); }

                // Auto-save to vault and capture file path
                let vaultFile = null;
                if (settings.auto_save && settings.vault_dir) {
                    try {
                        const vaultRes = await fetch('/api/vault/save', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ text: text.trim(), language: lang })
                        });
                        if (vaultRes.ok) {
                            const vaultData = await vaultRes.json();
                            vaultFile = vaultData.file;
                        }
                    } catch (e) { console.warn('Vault auto-save failed:', e); }
                }

                // Save to history with recording + vault links
                addToHistory(text.trim(), lang, recordingFile, vaultFile);
                // Auto-copy
                if (settings.auto_copy) navigator.clipboard.writeText(text.trim()).catch(() => { });
            }
        } catch (err) {
            console.error('Transcription error:', err);
            const isAbort = err.name === 'AbortError';
            const msg = isAbort ? 'Request timed out after 90 seconds. The backend may be overloaded or the audio file is too large.' : err.message;
            const errMsg = msg.replace(/\n/g, '<br>');
            const errorDiv = document.createElement('div');
            errorDiv.className = 'transcription-error';
            errorDiv.innerHTML = `<strong>⚠️ ${translateMode ? 'Translation' : 'Transcription'} Error</strong><br>${escapeHTML(errMsg)}`;
            transcriptionText.appendChild(errorDiv);
            placeholder.style.display = 'none';
        } finally {
            showProcessing(false);
        }
    }

    function formatTimestamp(seconds) {
        if (!seconds && seconds !== 0) return '';
        const m = Math.floor(seconds / 60);
        const s = Math.floor(seconds % 60);
        return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
    }

    function appendTranscription(text, isHTML) {
        placeholder.classList.add('hidden');
        // Clear display only — show most recent transcription
        transcriptionText.innerHTML = '';

        const now = new Date();
        const time = now.toLocaleTimeString();
        const entry = document.createElement('div');
        entry.className = 'entry';
        const header = `<span class="log-stardate">${time}</span>`;
        if (isHTML) {
            entry.innerHTML = header + '\n' + text;
        } else {
            entry.innerHTML = header + '\n' + escapeHTML(text);
        }
        transcriptionText.appendChild(entry);
        transcriptionText.scrollTop = transcriptionText.scrollHeight;
        // Set currentTranscription (plain text version)
        if (!isHTML) {
            currentTranscription = text;
        } else {
            const tmp = document.createElement('div');
            tmp.innerHTML = text;
            currentTranscription = tmp.textContent;
        }
        // NOTE: currentSegments is managed by the caller (transcribeAudio),
        // NOT here — do not clear it.
    }

    function escapeHTML(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }

    let processingTimer = null;
    function showProcessing(show, message) {
        processing.classList.toggle('active', show);
        recordBtn.disabled = show;
        // Update processing status text
        const statusEl = processing.querySelector('.processing-status');
        if (show && statusEl) {
            statusEl.textContent = message || 'Processing…';
            // Live elapsed timer
            let elapsed = 0;
            const timerEl = processing.querySelector('.processing-elapsed');
            if (timerEl) timerEl.textContent = '0s';
            clearInterval(processingTimer);
            processingTimer = setInterval(() => {
                elapsed++;
                if (timerEl) timerEl.textContent = `${elapsed}s`;
            }, 1000);
        } else {
            clearInterval(processingTimer);
        }
    }

    // --- Transcription history (selectable log entries) ---
    const historyList = document.getElementById('historyList');
    const historyCount = document.getElementById('historyCount');
    const historySelectBtn = document.getElementById('historySelectBtn');
    const historyBulkBar = document.getElementById('historyBulkBar');
    const bulkSelectAll = document.getElementById('bulkSelectAll');
    let selectMode = false;

    function addToHistory(text, language, recordingFile, vaultFile) {
        const entry = {
            text: text.substring(0, 500),
            language: language,
            timestamp: new Date().toISOString(),
            recording: recordingFile || null,
            vault_file: vaultFile || null
        };
        // Store segments for SRT/VTT export (compact: only start/end/text)
        if (currentSegments && currentSegments.length > 0) {
            entry.segments = currentSegments.slice(0, 200).map(s => ({
                start: s.start,
                end: s.end,
                text: (s.text || '').trim()
            }));
        }
        if (settings.show_stardates !== false) {
            entry.stardate = stardateDisplay.textContent.replace('Stardate ', '');
        }
        logHistory.unshift(entry);
        if (logHistory.length > 50) logHistory = logHistory.slice(0, 50);
        localStorage.setItem('captainslog_history', JSON.stringify(logHistory));
        renderHistory();
    }

    function renderHistory() {
        const limit = settings.history_limit || 0;
        const total = logHistory.length;
        const query = (document.getElementById('historySearch')?.value || '').toLowerCase().trim();
        const pinnedSection = document.getElementById('pinnedSection');
        const pinnedList = document.getElementById('pinnedList');

        // Separate pinned and unpinned
        const pinned = logHistory.map((e, i) => ({ ...e, _idx: i })).filter(e => e.pinned);
        const unpinned = logHistory.map((e, i) => ({ ...e, _idx: i })).filter(e => !e.pinned);

        // Apply history limit only to unpinned entries
        const limitedUnpinned = limit > 0 ? unpinned.slice(0, limit) : unpinned;

        // Search applies globally
        let searchPinned = pinned;
        let searchRecent = limitedUnpinned;
        if (query) {
            searchPinned = pinned.filter(e => e.text.toLowerCase().includes(query));
            searchRecent = logHistory.map((e, i) => ({ ...e, _idx: i }))
                .filter(e => !e.pinned && e.text.toLowerCase().includes(query));
        }

        // Update count
        const shownCount = searchPinned.length + searchRecent.length;
        let countText = query ? `(${shownCount} matching)` :
            limit > 0 && total > limit ? `(${limitedUnpinned.length} of ${unpinned.length})` : `(${unpinned.length})`;
        historyCount.textContent = countText;

        // Pinned section (its own island)
        if (pinnedSection && pinnedList) {
            if (searchPinned.length > 0) {
                pinnedSection.style.display = '';
                pinnedList.innerHTML = searchPinned.map(entry => renderEntry(entry, entry._idx)).join('');
            } else {
                pinnedSection.style.display = 'none';
                pinnedList.innerHTML = '';
            }
        }

        // Recent (unpinned)
        if (searchRecent.length === 0) {
            historyList.innerHTML = query
                ? '<div class="history-empty">No matches found.</div>'
                : '<div class="history-empty">No logs yet. Record your first entry above.</div>';
            return;
        }
        historyList.innerHTML = searchRecent.map(entry => renderEntry(entry, entry._idx)).join('');
    }

    function renderEntry(entry, origIndex) {
        const time = new Date(entry.timestamp);
        const timeOpts = { hour: '2-digit', minute: '2-digit' };
        if (settings.time_format === '12h') timeOpts.hour12 = true;
        else if (settings.time_format === '24h') timeOpts.hour12 = false;
        const timeStr = time.toLocaleTimeString([], timeOpts);
        const dateStr = time.toLocaleDateString([], { month: 'short', day: 'numeric' });
        const preview = entry.text.length > 120 ? entry.text.substring(0, 120) + '…' : entry.text;
        const stardate = entry.stardate ? `<span class="log-entry-stardate">SD ${entry.stardate}</span>` : '';
        const isPinned = entry.pinned;

        // Visible: Star + Play + Copy + Export
        let actions = '';
        actions += `<button class="log-action ${isPinned ? 'pinned' : ''}" title="${isPinned ? 'Unpin' : 'Pin to top'}" data-pin="${origIndex}">
                <svg viewBox="0 0 24 24" fill="${isPinned ? 'currentColor' : 'none'}" stroke="currentColor" stroke-width="2"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>
            </button>`;
        if (entry.recording) {
            actions += `<button class="log-action" title="Play recording" data-play="${entry.recording}">
                <svg viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21"/></svg>
            </button>`;
        }
        actions += `<button class="log-action" title="Copy text" data-copy="${origIndex}">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
            </button>`;
        actions += `<button class="log-action" title="Export (${(settings.default_export_format || 'txt').toUpperCase()})" data-export="${origIndex}">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
            </button>`;

        // Overflow menu (⋮)
        let menuItems = '';
        menuItems += `<button role="menuitem" class="overflow-item" data-export-as="${origIndex}">📥 Export As…</button>`;
        if (entry.recording) {
            menuItems += `<button role="menuitem" class="overflow-item" data-goto-audio="${entry.recording}">📂 Open audio folder</button>`;
        }
        if (entry.vault_file) {
            menuItems += `<button role="menuitem" class="overflow-item" data-goto-text="${entry.vault_file}">📄 Open text file</button>`;
        }
        menuItems += `<button role="menuitem" class="overflow-item" data-save-pkm="${origIndex}">💾 Save to PKM</button>`;
        menuItems += `<hr class="overflow-divider">`;
        menuItems += `<button role="menuitem" class="overflow-item danger" data-delete="${origIndex}">🗑️ Delete</button>`;

        actions += `<div class="overflow-menu">
                <button class="log-action overflow-trigger" title="More actions" aria-haspopup="true" aria-expanded="false">
                    <svg viewBox="0 0 24 24" fill="currentColor" width="14" height="14"><circle cx="12" cy="5" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="12" cy="19" r="2"/></svg>
                </button>
                <div class="overflow-panel" role="menu" aria-label="Entry actions" style="display:none;">
                    ${menuItems}
                </div>
            </div>`;

        return `<div class="log-entry ${isPinned ? 'log-entry-pinned' : ''}" data-index="${origIndex}">
                <input type="checkbox" class="log-entry-checkbox" data-check="${origIndex}">
                <div class="log-entry-body">
                    <div class="log-entry-meta">
                        <span class="log-entry-time">${timeStr}</span>
                        <span>${dateStr}</span>
                        ${stardate}
                        ${isPinned ? '<span class="pin-badge">📌</span>' : ''}
                        ${entry.recording ? '<span>🎙️</span>' : ''}
                        ${entry.vault_file ? '<span>📁</span>' : ''}
                    </div>
                    <div class="log-entry-text">${preview}</div>
                </div>
                <div class="log-entry-actions">${actions}</div>
            </div>`;
    }

    // Select mode toggle
    historySelectBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        selectMode = !selectMode;
        historySelectBtn.classList.toggle('active', selectMode);
        historySelectBtn.textContent = selectMode ? 'Done' : 'Select';
        historyBulkBar.classList.toggle('hidden', !selectMode);
        historyList.classList.toggle('select-mode', selectMode);
        bulkSelectAll.checked = false;
    });

    // Select All
    bulkSelectAll.addEventListener('change', () => {
        const boxes = historyList.querySelectorAll('.log-entry-checkbox');
        boxes.forEach(cb => {
            cb.checked = bulkSelectAll.checked;
            cb.closest('.log-entry').classList.toggle('checked', bulkSelectAll.checked);
        });
    });

    function getSelectedIndices() {
        const boxes = historyList.querySelectorAll('.log-entry-checkbox:checked');
        return Array.from(boxes).map(cb => parseInt(cb.dataset.check));
    }

    // Bulk Send to PKM
    el('bulkSavePKM').addEventListener('click', async () => {
        const indices = getSelectedIndices();
        if (!indices.length) return;
        for (const idx of indices) {
            const entry = logHistory[idx];
            if (!entry) continue;
            try {
                await fetch('/api/vault/save', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ text: entry.text, language: entry.language || 'en' })
                });
            } catch (e) { console.warn('Bulk vault save failed for entry:', e); }
        }
        flashButton(el('bulkSavePKM'), `Saved ${indices.length}!`, 'success');
    });

    // Bulk Copy
    el('bulkCopy').addEventListener('click', () => {
        const indices = getSelectedIndices();
        if (!indices.length) return;
        const combined = indices.map(idx => logHistory[idx]?.text).filter(Boolean).join('\n\n---\n\n');
        navigator.clipboard.writeText(combined).then(() => {
            flashButton(el('bulkCopy'), `Copied ${indices.length}!`, 'success');
        });
    });

    // Bulk Export
    const bulkExportBtn = el('bulkExport');
    if (bulkExportBtn) {
        bulkExportBtn.addEventListener('click', () => {
            const indices = getSelectedIndices();
            if (!indices.length) return;
            const combined = indices.map(idx => {
                const entry = logHistory[idx];
                if (!entry) return '';
                const date = new Date(entry.timestamp);
                return `## ${date.toLocaleString()}\n\n${entry.text}`;
            }).filter(Boolean).join('\n\n---\n\n');
            doExport(combined, [], getDefaultExportFormat(), `${settings.file_title || 'Dictation'}_bulk`);
            flashButton(bulkExportBtn, `Exported ${indices.length}!`, 'success');
        });
    }

    // Bulk Delete
    const bulkDeleteBtn = el('bulkDelete');
    if (bulkDeleteBtn) {
        bulkDeleteBtn.addEventListener('click', () => {
            const indices = getSelectedIndices();
            if (!indices.length) return;
            if (!confirm(`Delete ${indices.length} transcription${indices.length > 1 ? 's' : ''}? This cannot be undone.`)) return;
            // Delete in reverse order to preserve indices
            indices.sort((a, b) => b - a).forEach(idx => logHistory.splice(idx, 1));
            localStorage.setItem('captainslog_history', JSON.stringify(logHistory));
            renderHistory();
            // Exit select mode
            selectMode = false;
            historySelectBtn.classList.remove('active');
            historySelectBtn.textContent = 'Select';
            historyBulkBar.classList.add('hidden');
            historyList.classList.remove('select-mode');
            flashButton(bulkDeleteBtn, `Deleted ${indices.length}!`, 'success');
        });
    }
    // Search history
    const historySearch = document.getElementById('historySearch');
    if (historySearch) {
        historySearch.addEventListener('input', () => renderHistory());
    }

    // --- Pinned select mode ---
    const pinnedSelectBtn = el('pinnedSelectBtn');
    const pinnedBulkBar = el('pinnedBulkBar');
    const pinnedListEl = document.getElementById('pinnedList');
    const pinnedSelectAll = el('pinnedSelectAll');
    let pinnedSelectMode = false;

    if (pinnedSelectBtn) {
        pinnedSelectBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            pinnedSelectMode = !pinnedSelectMode;
            pinnedSelectBtn.classList.toggle('active', pinnedSelectMode);
            pinnedSelectBtn.textContent = pinnedSelectMode ? 'Done' : 'Select';
            pinnedBulkBar.classList.toggle('hidden', !pinnedSelectMode);
            if (pinnedListEl) pinnedListEl.classList.toggle('select-mode', pinnedSelectMode);
            if (pinnedSelectAll) pinnedSelectAll.checked = false;
        });
    }

    if (pinnedSelectAll) {
        pinnedSelectAll.addEventListener('change', () => {
            if (!pinnedListEl) return;
            const boxes = pinnedListEl.querySelectorAll('.log-entry-checkbox');
            boxes.forEach(cb => {
                cb.checked = pinnedSelectAll.checked;
                cb.closest('.log-entry').classList.toggle('checked', pinnedSelectAll.checked);
            });
        });
    }

    function getPinnedSelectedIndices() {
        if (!pinnedListEl) return [];
        const boxes = pinnedListEl.querySelectorAll('.log-entry-checkbox:checked');
        return Array.from(boxes).map(cb => parseInt(cb.dataset.check));
    }

    function exitPinnedSelectMode() {
        pinnedSelectMode = false;
        if (pinnedSelectBtn) { pinnedSelectBtn.classList.remove('active'); pinnedSelectBtn.textContent = 'Select'; }
        if (pinnedBulkBar) pinnedBulkBar.classList.add('hidden');
        if (pinnedListEl) pinnedListEl.classList.remove('select-mode');
    }

    // Pinned bulk Copy
    const pinnedBulkCopyBtn = el('pinnedBulkCopy');
    if (pinnedBulkCopyBtn) {
        pinnedBulkCopyBtn.addEventListener('click', () => {
            const indices = getPinnedSelectedIndices();
            if (!indices.length) return;
            const combined = indices.map(idx => logHistory[idx]?.text).filter(Boolean).join('\n\n---\n\n');
            navigator.clipboard.writeText(combined).then(() => flashButton(pinnedBulkCopyBtn, `Copied ${indices.length}!`, 'success'));
        });
    }

    // Pinned bulk Export
    const pinnedBulkExportBtn = el('pinnedBulkExport');
    if (pinnedBulkExportBtn) {
        pinnedBulkExportBtn.addEventListener('click', () => {
            const indices = getPinnedSelectedIndices();
            if (!indices.length) return;
            const combined = indices.map(idx => {
                const entry = logHistory[idx];
                if (!entry) return '';
                const date = new Date(entry.timestamp);
                return `## ${date.toLocaleString()}\n\n${entry.text}`;
            }).filter(Boolean).join('\n\n---\n\n');
            doExport(combined, [], getDefaultExportFormat(), `${settings.file_title || 'Dictation'}_pinned`);
            flashButton(pinnedBulkExportBtn, `Exported ${indices.length}!`, 'success');
        });
    }

    // Pinned bulk Unpin
    const pinnedBulkUnpinBtn = el('pinnedBulkUnpin');
    if (pinnedBulkUnpinBtn) {
        pinnedBulkUnpinBtn.addEventListener('click', () => {
            const indices = getPinnedSelectedIndices();
            if (!indices.length) return;
            indices.forEach(idx => { if (logHistory[idx]) logHistory[idx].pinned = false; });
            localStorage.setItem('captainslog_history', JSON.stringify(logHistory));
            renderHistory();
            exitPinnedSelectMode();
            flashButton(pinnedBulkUnpinBtn, `Unpinned ${indices.length}!`, 'success');
        });
    }

    // Pinned bulk Delete
    const pinnedBulkDeleteBtn = el('pinnedBulkDelete');
    if (pinnedBulkDeleteBtn) {
        pinnedBulkDeleteBtn.addEventListener('click', () => {
            const indices = getPinnedSelectedIndices();
            if (!indices.length) return;
            if (!confirm(`Delete ${indices.length} pinned transcription${indices.length > 1 ? 's' : ''}? This cannot be undone.`)) return;
            indices.sort((a, b) => b - a).forEach(idx => logHistory.splice(idx, 1));
            localStorage.setItem('captainslog_history', JSON.stringify(logHistory));
            renderHistory();
            exitPinnedSelectMode();
            flashButton(pinnedBulkDeleteBtn, `Deleted ${indices.length}!`, 'success');
        });
    }

    // History card interactions (shared handler for both lists)
    function handleEntryClick(e) {
        // Pin/unpin
        const pinBtn = e.target.closest('[data-pin]');
        if (pinBtn) {
            e.stopPropagation();
            const idx = parseInt(pinBtn.dataset.pin, 10);
            if (logHistory[idx]) {
                logHistory[idx].pinned = !logHistory[idx].pinned;
                localStorage.setItem('captainslog_history', JSON.stringify(logHistory));
                renderHistory();
            }
            return;
        }

        // Play button
        const playBtn = e.target.closest('[data-play]');
        if (playBtn) {
            e.stopPropagation();
            const filename = playBtn.dataset.play;
            const entry = playBtn.closest('.log-entry');
            let player = entry.querySelector('audio');
            if (player) {
                player.remove();
                return;
            }
            player = document.createElement('audio');
            player.className = 'log-audio-player';
            player.controls = true;
            player.src = `/api/recordings/${filename}`;
            entry.querySelector('.log-entry-body').appendChild(player);
            player.play().catch(() => { });
            return;
        }

        // Go to audio
        const gotoAudio = e.target.closest('[data-goto-audio]');
        if (gotoAudio) {
            e.stopPropagation();
            const filename = gotoAudio.dataset.gotoAudio;
            const path = (settings.config_dir || '~/.config/captainslog') + '/recordings/' + filename;
            fetch('/api/open', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path })
            }).then(r => {
                if (r.ok) { gotoAudio.title = 'Opened!'; }
                else { navigator.clipboard.writeText(path); gotoAudio.title = 'Path copied!'; }
                setTimeout(() => gotoAudio.title = 'Go to audio file', 1500);
            }).catch(() => {
                navigator.clipboard.writeText(path);
                gotoAudio.title = 'Path copied!';
                setTimeout(() => gotoAudio.title = 'Go to audio file', 1500);
            });
            return;
        }

        // Go to text
        const gotoText = e.target.closest('[data-goto-text]');
        if (gotoText) {
            e.stopPropagation();
            const path = gotoText.dataset.gotoText;
            fetch('/api/open', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path })
            }).then(r => {
                if (r.ok) { gotoText.title = 'Opened!'; }
                else { navigator.clipboard.writeText(path); gotoText.title = 'Path copied!'; }
                setTimeout(() => gotoText.title = 'Go to text file', 1500);
            }).catch(() => {
                navigator.clipboard.writeText(path);
                gotoText.title = 'Path copied!';
                setTimeout(() => gotoText.title = 'Go to text file', 1500);
            });
            return;
        }

        // Copy button
        const copyBtn = e.target.closest('[data-copy]');
        if (copyBtn) {
            e.stopPropagation();
            const idx = parseInt(copyBtn.dataset.copy);
            if (logHistory[idx]) {
                const pureText = logHistory[idx].text.replace(/<[^>]*>/g, '').trim();
                navigator.clipboard.writeText(pureText).then(() => {
                    copyBtn.style.color = 'var(--accent)';
                    setTimeout(() => copyBtn.style.color = '', 1000);
                });
            }
            return;
        }

        // Export single entry (default format)
        const exportEntryBtn = e.target.closest('[data-export]');
        if (exportEntryBtn) {
            e.stopPropagation();
            closeAllOverflows();
            const idx = parseInt(exportEntryBtn.dataset.export);
            if (logHistory[idx]) {
                doExport(logHistory[idx].text, logHistory[idx].segments || [], getDefaultExportFormat());
            }
            return;
        }

        // Export As (per-entry overflow menu)
        const exportAsEntry = e.target.closest('[data-export-as]');
        if (exportAsEntry) {
            e.stopPropagation();
            closeAllOverflows();
            const idx = parseInt(exportAsEntry.dataset.exportAs);
            if (logHistory[idx]) {
                showExportAsDialog(logHistory[idx].text, logHistory[idx].segments || []);
            }
            return;
        }

        // Save to PKM from overflow
        const savePkmBtn = e.target.closest('[data-save-pkm]');
        if (savePkmBtn) {
            e.stopPropagation();
            closeAllOverflows();
            const idx = parseInt(savePkmBtn.dataset.savePkm);
            const entry = logHistory[idx];
            if (entry) {
                fetch('/api/vault/save', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ text: entry.text, language: entry.language || 'en' })
                }).then(r => r.json()).then(data => {
                    if (data.file) {
                        logHistory[idx].vault_file = data.file;
                        localStorage.setItem('captainslog_history', JSON.stringify(logHistory));
                        renderHistory();
                    }
                }).catch(err => console.warn('PKM save failed:', err));
            }
            return;
        }

        // Delete with confirmation
        const deleteBtn = e.target.closest('[data-delete]');
        if (deleteBtn) {
            e.stopPropagation();
            closeAllOverflows();
            const idx = parseInt(deleteBtn.dataset.delete);
            if (logHistory[idx] && confirm('Delete this transcription?\n\n"' + logHistory[idx].text.substring(0, 80) + '…"')) {
                logHistory.splice(idx, 1);
                localStorage.setItem('captainslog_history', JSON.stringify(logHistory));
                renderHistory();
            }
            return;
        }

        // Overflow menu trigger (⋮)
        const trigger = e.target.closest('.overflow-trigger');
        if (trigger) {
            e.stopPropagation();
            const panel = trigger.nextElementSibling;
            const isOpen = panel.style.display !== 'none';
            closeAllOverflows();
            if (!isOpen) {
                panel.style.display = 'flex';
                trigger.setAttribute('aria-expanded', 'true');
                const firstItem = panel.querySelector('[role=menuitem]');
                if (firstItem) firstItem.focus();
            }
            return;
        }
    }

    // Attach handler to both history lists
    historyList.addEventListener('click', handleEntryClick);
    if (pinnedListEl) pinnedListEl.addEventListener('click', handleEntryClick);

    // Overflow keyboard navigation (both lists)
    function handleOverflowKeydown(e) {
        const panel = e.target.closest('.overflow-panel');
        if (!panel) return;
        const items = [...panel.querySelectorAll('[role=menuitem]')];
        const idx = items.indexOf(e.target);
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            items[(idx + 1) % items.length]?.focus();
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            items[(idx - 1 + items.length) % items.length]?.focus();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            closeAllOverflows();
        }
    }
    historyList.addEventListener('keydown', handleOverflowKeydown);
    if (pinnedListEl) pinnedListEl.addEventListener('keydown', handleOverflowKeydown);
    // Close overflows on outside click
    document.addEventListener('click', (e) => {
        if (!e.target.closest('.overflow-menu')) closeAllOverflows();
    });

    function closeAllOverflows() {
        document.querySelectorAll('.overflow-panel').forEach(p => p.style.display = 'none');
        document.querySelectorAll('.overflow-trigger').forEach(t => t.setAttribute('aria-expanded', 'false'));
    }

    // Initial render
    renderHistory();

    // --- Actions ---
    copyBtn.addEventListener('click', () => {
        if (!currentTranscription) { flashButton(copyBtn, 'Nothing to copy', 'error'); return; }
        // Pure text only — strip any HTML tags defensively, no timestamps, no headers
        const pureText = currentTranscription.replace(/<[^>]*>/g, '').trim();
        navigator.clipboard.writeText(pureText)
            .then(() => flashButton(copyBtn, 'Copied!', 'success'))
            .catch(err => { console.error('Copy failed:', err); flashButton(copyBtn, 'Copy failed', 'error'); });
    });

    function exportContent(text, segments, format) {
        const pureText = (text || '').replace(/<[^>]*>/g, '').trim();
        const isRich = (settings.export_mode || 'rich') === 'rich';

        // Build rich text from segments if available and Rich mode is active
        function buildRichText() {
            if (!isRich || !segments || segments.length === 0) return pureText;
            return segments.map(seg => {
                let line = '';
                if (seg.speaker !== undefined && seg.speaker !== null) {
                    line += `SPEAKER ${seg.speaker + 1}: `;
                }
                if (seg.start !== undefined) {
                    const m = Math.floor(seg.start / 60);
                    const s = Math.floor(seg.start % 60);
                    line += `[${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}] `;
                }
                line += (seg.text || '').trim();
                return line;
            }).join('\n');
        }

        switch (format) {
            case 'srt': return generateSRT(segments, pureText);
            case 'vtt': return 'WEBVTT\n\n' + generateVTT(segments, pureText);
            case 'json': {
                const obj = {
                    text: pureText,
                    language: settings.language || 'en',
                    timestamp: new Date().toISOString()
                };
                if (segments && segments.length > 0) {
                    obj.segments = segments.map(s => ({
                        start: s.start,
                        end: s.end,
                        text: (s.text || '').trim(),
                        ...(s.speaker !== undefined ? { speaker: s.speaker } : {})
                    }));
                }
                return JSON.stringify(obj, null, 2);
            }
            case 'md': {
                const now = new Date();
                const body = buildRichText();
                return `---\ntitle: ${settings.file_title || 'Dictation'}\ndate: ${now.toISOString()}\ntags: [dictation]\n---\n\n${body}\n`;
            }
            default: return buildRichText(); // txt
        }
    }

    function doExport(text, segments, format, filenameBase) {
        const content = exportContent(text, segments, format);
        const ext = format;
        const mimeMap = {
            'json': 'application/json',
            'md': 'text/markdown',
            'vtt': 'text/vtt',
            'srt': 'application/x-subrip'
        };
        const mime = mimeMap[format] || 'text/plain';
        downloadTextFile(content, `${filenameBase || settings.file_title || 'Dictation'}_${Date.now()}.${ext}`, mime);
    }

    function getDefaultExportFormat() {
        return settings.default_export_format || 'txt';
    }

    // Export As dialog
    const exportAsModal = document.getElementById('exportAsModal');
    const exportFormatList = document.getElementById('exportFormatList');
    let pendingExportData = null;

    function showExportAsDialog(text, segments, filenameBase) {
        pendingExportData = { text, segments, filenameBase };
        exportAsModal.classList.remove('hidden');

        // Disable subtitle formats in Pure mode (no segment data to work with)
        const isPure = settings.export_mode === 'pure';
        const buttons = exportFormatList.querySelectorAll('[data-fmt]');
        buttons.forEach(btn => {
            const fmt = btn.dataset.fmt;
            if (fmt === 'srt' || fmt === 'vtt') {
                btn.disabled = isPure;
                btn.title = isPure ? 'Subtitle formats require Rich export mode (timestamps)' : '';
                btn.style.opacity = isPure ? '0.4' : '';
            }
        });

        const firstBtn = exportFormatList.querySelector('[data-fmt]:not(:disabled)');
        if (firstBtn) firstBtn.focus();
    }

    if (exportFormatList) {
        exportFormatList.addEventListener('click', (e) => {
            const btn = e.target.closest('[data-fmt]');
            if (!btn || !pendingExportData) return;
            const fmt = btn.dataset.fmt;
            doExport(pendingExportData.text, pendingExportData.segments, fmt, pendingExportData.filenameBase);

            // Set as default if checkbox checked
            const setDefault = document.getElementById('exportSetDefault');
            if (setDefault && setDefault.checked) {
                settings.default_export_format = fmt;
                localStorage.setItem('captainslog_prefs', JSON.stringify(settings));
                fetch('/api/settings', {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(settings)
                }).catch(() => { });
                el('settExportFormat').value = fmt;
                renderHistory();
                setDefault.checked = false;
            }

            exportAsModal.classList.add('hidden');
            pendingExportData = null;
        });

        // Keyboard nav in format picker
        exportFormatList.addEventListener('keydown', (e) => {
            const items = [...exportFormatList.querySelectorAll('[data-fmt]')];
            const idx = items.indexOf(e.target);
            if (e.key === 'ArrowDown') { e.preventDefault(); items[(idx + 1) % items.length]?.focus(); }
            else if (e.key === 'ArrowUp') { e.preventDefault(); items[(idx - 1 + items.length) % items.length]?.focus(); }
            else if (e.key === 'Escape') { exportAsModal.classList.add('hidden'); pendingExportData = null; }
        });
    }

    // Close Export As on backdrop click
    if (exportAsModal) {
        exportAsModal.addEventListener('click', (e) => {
            if (e.target === exportAsModal) { exportAsModal.classList.add('hidden'); pendingExportData = null; }
        });
    }

    // Latest transcription Export button (default format)
    const exportBtn = document.getElementById('exportBtn');
    exportBtn.addEventListener('click', () => {
        if (!currentTranscription) { flashButton(exportBtn, 'Nothing to export', 'error'); return; }
        doExport(currentTranscription, currentSegments, getDefaultExportFormat());
    });

    // Latest transcription Export As button
    const exportAsBtn = document.getElementById('exportAsBtn');
    if (exportAsBtn) {
        exportAsBtn.addEventListener('click', () => {
            if (!currentTranscription) { flashButton(exportAsBtn, 'No transcription', 'error'); return; }
            showExportAsDialog(currentTranscription, currentSegments);
        });
    }

    function generateSRT(segments, fallbackText) {
        if (!segments || segments.length === 0) {
            return '1\n00:00:00,000 --> 00:00:10,000\n' + fallbackText + '\n';
        }
        return segments.map((s, i) => {
            const start = formatSrtTime(s.start);
            const end = formatSrtTime(s.end);
            const text = (s.text || '').trim() || '...';
            return `${i + 1}\n${start} --> ${end}\n${text}`;
        }).join('\n\n') + '\n';
    }

    function generateVTT(segments, fallbackText) {
        if (!segments || segments.length === 0) {
            return '1\n00:00:00.000 --> 00:00:10.000\n' + fallbackText + '\n';
        }
        return segments.map((s, i) => {
            const start = formatVttTime(s.start);
            const end = formatVttTime(s.end);
            const text = (s.text || '').trim() || '...';
            return `${i + 1}\n${start} --> ${end}\n${text}`;
        }).join('\n\n') + '\n';
    }

    function formatSrtTime(seconds) {
        if (typeof seconds !== 'number' || isNaN(seconds) || seconds < 0) seconds = 0;
        const totalMs = Math.round(seconds * 1000);
        const ms = totalMs % 1000;
        const totalSec = Math.floor(totalMs / 1000);
        const ss = totalSec % 60;
        const totalMin = Math.floor(totalSec / 60);
        const mm = totalMin % 60;
        const hh = Math.floor(totalMin / 60);
        return `${String(hh).padStart(2, '0')}:${String(mm).padStart(2, '0')}:${String(ss).padStart(2, '0')},${String(ms).padStart(3, '0')}`;
    }

    function formatVttTime(seconds) {
        // VTT uses dots for decimals, SRT uses commas
        return formatSrtTime(seconds).replace(',', '.');
    }

    saveBtn.addEventListener('click', async () => {
        if (!currentTranscription) return;
        try {
            const res = await fetch('/api/vault/save', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ text: currentTranscription, language: settings.language || 'en' })
            });
            if (res.ok) {
                flashButton(saveBtn, 'Logged!', 'success');
            } else {
                const data = await res.json();
                alert(data.error || 'Log failed — set vault directory in Preferences');
            }
        } catch (err) {
            alert('Log failed: ' + err.message);
        }
    });

    goToTextBtn.addEventListener('click', () => {
        if (!currentTranscription) return;
        const recent = logHistory.find(e => e.vault_file);
        if (recent) {
            fetch('/api/open', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: recent.vault_file })
            }).then(r => {
                flashButton(goToTextBtn, r.ok ? 'Opened!' : 'Path copied!', 'success');
                if (!r.ok) navigator.clipboard.writeText(recent.vault_file);
            }).catch(() => {
                navigator.clipboard.writeText(recent.vault_file);
                flashButton(goToTextBtn, 'Path copied!', 'success');
            });
        } else {
            flashButton(goToTextBtn, 'No saved text', '');
        }
    });

    goToAudioBtn.addEventListener('click', () => {
        if (!currentTranscription) return;
        const recent = logHistory.find(e => e.recording);
        if (recent) {
            fetch('/api/open', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ recording: recent.recording })
            }).then(r => {
                flashButton(goToAudioBtn, r.ok ? 'Opened!' : 'Path copied!', 'success');
                if (!r.ok) navigator.clipboard.writeText(recent.recording);
            }).catch(() => {
                navigator.clipboard.writeText(recent.recording);
                flashButton(goToAudioBtn, 'Path copied!', 'success');
            });
        } else {
            flashButton(goToAudioBtn, 'No recording', '');
        }
    });


    // LLM button handler — proxied through backend to avoid CORS
    if (llmBtn) {
        llmBtn.addEventListener('click', () => {
            if (!currentTranscription) { flashButton(llmBtn, 'Nothing to send', 'error'); return; }
            const savedTranscription = currentTranscription; // Preserve — AI response must not overwrite
            const model = settings.llm_model || 'llama3.2';
            const aiPrompt = 'Please review, correct errors, improve formatting, and respond:\n\n' + currentTranscription;
            flashButton(llmBtn, 'Sending…', '');
            fetch('/api/llm/chat', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ model, messages: [{ role: 'user', content: aiPrompt }], stream: false })
            }).then(r => {
                if (!r.ok) return r.json().then(e => { throw new Error(e.error || e.detail || 'LLM error'); });
                return r.json();
            }).then(d => {
                let aiText = '';
                if (d.choices && d.choices.length > 0) {
                    aiText = d.choices[0].message.content;
                } else if (d.response) {
                    aiText = d.response;
                }
                if (aiText) {
                    appendTranscription('\n\n🤖 AI:\n' + aiText, false);
                    // Restore original — AI response should not become the copyable text
                    currentTranscription = savedTranscription;
                }
                flashButton(llmBtn, 'Send to AI', '');
            }).catch(err => {
                flashButton(llmBtn, err.message || 'LLM error', 'error');
            });
        });
        // Show/hide based on settings
        llmBtn.style.display = settings.enable_llm ? '' : 'none';
    }

    // --- Keyboard shortcuts ---
    function toggleMiniMode() {
        document.body.classList.toggle('mini');
        const isMini = document.body.classList.contains('mini');
        const url = new URL(window.location);
        if (isMini) url.searchParams.set('mini', '');
        else url.searchParams.delete('mini');
        history.replaceState(null, '', url);
        // Resize window for standalone/PWA windows
        try {
            if (isMini) window.resizeTo(420, 500);
            else window.resizeTo(900, 700);
        } catch (e) { /* browser may block resizeTo */ }
    }

    document.addEventListener('keydown', (e) => {
        const inInput = e.target.closest('input, select, textarea, [contenteditable]');
        const isButton = e.target.closest('button');

        // Escape always works — close modals
        if (e.code === 'Escape') {
            settingsModal.classList.add('hidden');
            if (exportAsModal) { exportAsModal.classList.add('hidden'); pendingExportData = null; }
            closeAllOverflows();
            return;
        }

        // Don't intercept when typing in inputs (except Ctrl+S which always blocks browser save)
        if (e.ctrlKey && e.key === 's') {
            e.preventDefault();
            e.stopPropagation();
            if (!inInput && currentTranscription) saveBtn.click();
            return;
        }

        // All other shortcuts require not being in a text input
        if (inInput) return;

        switch (true) {
            case e.code === 'Space':
                e.preventDefault();
                e.stopPropagation();
                toggleRecording();
                break;
            case (e.key === 'm' || e.key === 'M') && !e.ctrlKey && !e.metaKey:
                e.preventDefault();
                e.stopPropagation();
                toggleMiniMode();
                break;
            case e.key === ',':
                e.preventDefault();
                e.stopPropagation();
                applySettings();
                settingsModal.classList.remove('hidden');
                break;
            case e.ctrlKey && e.key === 'c' && !!currentTranscription:
                e.preventDefault();
                e.stopPropagation();
                {
                    const pureText = currentTranscription.replace(/<[^>]*>/g, '').trim();
                    navigator.clipboard.writeText(pureText);
                }
                flashButton(copyBtn, 'Copied!', 'success');
                break;
        }
    }, { capture: true });

    // --- Mini UI mode ---
    if (new URLSearchParams(window.location.search).has('mini')) {
        document.body.classList.add('mini');
    }
    const miniToggle = document.getElementById('miniToggle');
    if (miniToggle) miniToggle.addEventListener('click', toggleMiniMode);

    // --- Helpers ---
    function flashButton(btn, text, cls) {
        btn.classList.add(cls);
        const origHTML = btn.innerHTML;
        const svgEl = btn.querySelector('svg');
        btn.innerHTML = (svgEl ? svgEl.outerHTML : '') + ' ' + text;
        setTimeout(() => { btn.classList.remove(cls); btn.innerHTML = origHTML; }, 2000);
    }

    function downloadTextFile(text, filename, mime) {
        const blob = new Blob([text], { type: mime || 'text/plain' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        a.click();
        URL.revokeObjectURL(url);
    }

    // --- PWA install ---
    let deferredInstallPrompt = null;
    window.addEventListener('beforeinstallprompt', (e) => {
        e.preventDefault();
        deferredInstallPrompt = e;
        console.log('PWA install available');
    });

    if ('serviceWorker' in navigator) {
        navigator.serviceWorker.register('/sw.js').catch((e) => { console.warn('SW registration failed:', e); });
    }

})();
