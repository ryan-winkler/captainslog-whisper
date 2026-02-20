// Captain's Log — Browser client
// Handles recording, transcription, stardates, history, settings, and AI forwarding.

(function () {
    'use strict';

    // --- DOM refs ---
    const recordBtn = document.getElementById('recordBtn');
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
    const copyBtn = document.getElementById('copyBtn');
    const saveBtn = document.getElementById('saveBtn');
    const downloadBtn = document.getElementById('downloadBtn');
    const downloadFormat = document.getElementById('downloadFormat');
    const clearBtn = document.getElementById('clearBtn');
    const languageSelect = document.getElementById('languageSelect');
    const backendStatus = document.getElementById('backendStatus');
    const stardateDisplay = document.getElementById('stardateDisplay');
    const settingsBtn = document.getElementById('settingsBtn');
    const settingsModal = document.getElementById('settingsModal');
    const closeSettings = document.getElementById('closeSettings');
    const saveSettings = document.getElementById('saveSettings');
    const resetSettings = document.getElementById('resetSettings');

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
        file_title: 'Dictation',
        whisper_url: '',
        ollama_url: ''
    };

    let settings = { ...defaults };

    // --- State ---
    let mediaRecorder = null;
    let audioChunks = [];
    let isRecording = false;
    let timerInterval = null;
    let startTime = 0;
    let audioContext = null;
    let analyser = null;
    let animationId = null;
    let currentTranscription = '';

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
            try { Object.assign(settings, JSON.parse(stored)); } catch { }
        }
        applySettings();
        fetch('/api/settings').then(r => r.json()).then(data => {
            Object.assign(settings, data);
            applySettings();
        }).catch(() => { });
    }

    function applySettings() {
        languageSelect.value = settings.language || 'en';
        el('settVaultDir').value = settings.vault_dir || '';
        el('settDownloadDir').value = settings.download_dir || '';
        el('settLanguage').value = settings.language || 'en';
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
        el('settOllamaURL').value = settings.ollama_url || '';
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
        settings.ollama_url = el('settOllamaURL').value.trim();
        languageSelect.value = settings.language;
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

    // --- Settings modal ---
    settingsBtn.addEventListener('click', () => { applySettings(); fetchModels(); settingsModal.classList.remove('hidden'); });
    closeSettings.addEventListener('click', () => settingsModal.classList.add('hidden'));
    settingsModal.addEventListener('click', (e) => { if (e.target === settingsModal) settingsModal.classList.add('hidden'); });
    saveSettings.addEventListener('click', () => { saveSettingsToServer(); setTimeout(() => settingsModal.classList.add('hidden'), 600); });
    resetSettings.addEventListener('click', () => { settings = { ...defaults }; applySettings(); });
    languageSelect.addEventListener('change', () => {
        settings.language = languageSelect.value;
        localStorage.setItem('captainslog_prefs', JSON.stringify(settings));
    });

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
        try {
            const stream = await navigator.mediaDevices.getUserMedia({
                audio: { channelCount: 1, sampleRate: 16000, echoCancellation: true, noiseSuppression: true }
            });
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
        } catch (err) {
            console.error('Microphone access denied:', err);
            if (location.protocol === 'http:' && location.hostname !== 'localhost' && location.hostname !== '127.0.0.1') {
                alert('⚠️ Microphone blocked — HTTPS required\n\nChrome blocks mic on non-HTTPS origins.\n\nFix:\n1. Use http://localhost:8090\n2. Enable CAPTAINSLOG_ENABLE_TLS=true\n3. chrome://flags/#unsafely-treat-insecure-origin-as-secure');
            } else {
                alert('Microphone access required. Please allow and try again.');
            }
        }
    }

    function stopRecording() {
        if (mediaRecorder && mediaRecorder.state !== 'inactive') mediaRecorder.stop();
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

    function updateTimer() {
        const elapsed = Math.floor((Date.now() - startTime) / 1000);
        timer.textContent = `${String(Math.floor(elapsed / 60)).padStart(2, '0')}:${String(elapsed % 60).padStart(2, '0')}`;
    }

    // --- Waveform ---
    function drawWaveform() {
        if (!analyser) return;
        const ctx = waveform.getContext('2d');
        const bufferLength = analyser.frequencyBinCount;
        const dataArray = new Uint8Array(bufferLength);
        const width = waveform.width;
        const height = waveform.height;
        function draw() {
            animationId = requestAnimationFrame(draw);
            analyser.getByteTimeDomainData(dataArray);
            ctx.fillStyle = 'rgba(10, 14, 26, 0.3)';
            ctx.fillRect(0, 0, width, height);
            ctx.lineWidth = 2;
            ctx.strokeStyle = '#c49a6c';
            ctx.beginPath();
            const sliceWidth = width / bufferLength;
            let x = 0;
            for (let i = 0; i < bufferLength; i++) {
                const v = dataArray[i] / 128.0;
                const y = (v * height) / 2;
                if (i === 0) ctx.moveTo(x, y);
                else ctx.lineTo(x, y);
                x += sliceWidth;
            }
            ctx.lineTo(width, height / 2);
            ctx.stroke();
        }
        draw();
    }

    // --- File upload ---
    browseBtn.addEventListener('click', (e) => { e.preventDefault(); fileInput.click(); });
    fileInput.addEventListener('change', (e) => { if (e.target.files[0]) transcribeAudio(e.target.files[0]); });
    uploadZone.addEventListener('click', () => fileInput.click());
    uploadZone.addEventListener('dragover', (e) => { e.preventDefault(); uploadZone.classList.add('dragover'); });
    uploadZone.addEventListener('dragleave', () => uploadZone.classList.remove('dragover'));
    uploadZone.addEventListener('drop', (e) => {
        e.preventDefault();
        uploadZone.classList.remove('dragover');
        if (e.dataTransfer.files[0]) transcribeAudio(e.dataTransfer.files[0]);
    });

    // --- Transcription ---
    async function transcribeAudio(audioBlob) {
        showProcessing(true);
        const formData = new FormData();
        formData.append('file', audioBlob, 'recording.webm');
        formData.append('response_format', 'verbose_json');
        const lang = languageSelect.value;
        if (lang && lang !== 'und') formData.append('language', lang);
        if (settings.prompt) formData.append('prompt', settings.prompt);
        if (settings.vad_filter) formData.append('vad_filter', 'true');

        try {
            const res = await fetch('/v1/audio/transcriptions', { method: 'POST', body: formData });
            if (!res.ok) throw new Error(`Transcription failed: ${res.status}`);
            const data = await res.json();
            let text = data.text || '';

            // Handle verbose_json with segments (word-level timestamps / speaker labels)
            if (data.segments && data.segments.length > 0) {
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
                    line += seg.text;
                    return line;
                }).join('\n');
                appendTranscription(formatted, true);
                text = data.segments.map(s => s.text).join(' ');
            } else if (text.trim()) {
                appendTranscription(text.trim(), false);
            } else {
                appendTranscription('(No speech detected)', false);
            }

            if (text.trim()) {
                // Save to history
                addToHistory(text.trim(), lang);
                // Auto-actions
                if (settings.auto_copy) navigator.clipboard.writeText(text.trim()).catch(() => { });
                if (settings.auto_save && settings.vault_dir) {
                    fetch('/api/vault/save', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ text: text.trim(), language: lang })
                    }).catch(() => { });
                }
            }
        } catch (err) {
            console.error('Transcription error:', err);
            appendTranscription(`⚠️ Error: ${err.message}`, false);
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
        if (!isHTML) {
            currentTranscription += (currentTranscription ? '\n\n' : '') + text;
        } else {
            // Strip HTML for plain text version
            const tmp = document.createElement('div');
            tmp.innerHTML = text;
            currentTranscription += (currentTranscription ? '\n\n' : '') + tmp.textContent;
        }
    }

    function escapeHTML(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }

    function showProcessing(show) {
        processing.classList.toggle('active', show);
        recordBtn.disabled = show;
    }

    // --- Transcription history (Clipy-inspired) ---
    function addToHistory(text, language) {
        const entry = {
            text: text.substring(0, 500),
            language: language,
            timestamp: new Date().toISOString()
        };
        if (settings.show_stardates !== false) {
            entry.stardate = stardateDisplay.textContent.replace('Stardate ', '');
        }
        logHistory.unshift(entry);
        if (logHistory.length > 50) logHistory = logHistory.slice(0, 50);
        localStorage.setItem('captainslog_history', JSON.stringify(logHistory));
    }

    // --- Actions ---
    copyBtn.addEventListener('click', async () => {
        if (!currentTranscription) return;
        await navigator.clipboard.writeText(currentTranscription);
        flashButton(copyBtn, 'Copied!', 'success');
    });

    saveBtn.addEventListener('click', async () => {
        if (!currentTranscription) return;
        try {
            const res = await fetch('/api/vault/save', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ text: currentTranscription, language: languageSelect.value })
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

    downloadBtn.addEventListener('click', () => {
        if (!currentTranscription) return;
        const format = downloadFormat.value;
        const blob = new Blob([currentTranscription], { type: 'text/plain' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `captains-log-${new Date().toISOString().slice(0, 16)}.${format}`;
        a.click();
        URL.revokeObjectURL(url);
    });

    clearBtn.addEventListener('click', () => {
        transcriptionText.innerHTML = '';
        placeholder.classList.remove('hidden');
        currentTranscription = '';
        timer.textContent = '00:00';
    });

    // --- Keyboard shortcuts ---
    document.addEventListener('keydown', (e) => {
        const inInput = e.target.closest('input, select, textarea, button');
        if (e.code === 'Space' && !inInput) { e.preventDefault(); toggleRecording(); }
        if (e.code === 'Escape') settingsModal.classList.add('hidden');
        if (e.key === ',' && !inInput) { e.preventDefault(); applySettings(); settingsModal.classList.remove('hidden'); }
        if (e.ctrlKey && e.key === 'c' && !inInput && currentTranscription) {
            e.preventDefault();
            navigator.clipboard.writeText(currentTranscription);
            flashButton(copyBtn, 'Copied!', 'success');
        }
        if (e.ctrlKey && e.key === 's' && !inInput) { e.preventDefault(); if (currentTranscription) saveBtn.click(); }
    });

    // --- AI send buttons ---
    const aiPromptPrefix = "The following is a speech-to-text transcription from Captain's Log. Please review, correct errors, improve formatting, and respond:\n\n";

    el('sendOllama').addEventListener('click', async () => {
        if (!currentTranscription) return;
        flashButton(el('sendOllama'), 'Sending...', 'success');
        try {
            const ollamaUrl = settings.ollama_url || 'http://127.0.0.1:11434';
            const res = await fetch(ollamaUrl + '/api/generate', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ model: 'llama3.2', prompt: aiPromptPrefix + currentTranscription, stream: false })
            });
            const data = await res.json();
            if (data.response) appendTranscription('🦙 Ollama:\n' + data.response, false);
        } catch {
            alert('Ollama unreachable at ' + (settings.ollama_url || 'http://127.0.0.1:11434'));
        }
    });

    el('sendZeroClaw').addEventListener('click', async () => {
        if (!currentTranscription) return;
        try {
            const res = await fetch('http://127.0.0.1:3000/api/message', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ message: aiPromptPrefix + currentTranscription })
            });
            if (res.ok) {
                const data = await res.json();
                appendTranscription('🤖 ZeroClaw:\n' + (data.response || data.message || JSON.stringify(data)), false);
            } else throw new Error('ZeroClaw returned ' + res.status);
        } catch {
            await navigator.clipboard.writeText(currentTranscription);
            flashButton(el('sendZeroClaw'), 'Copied — paste in ZeroClaw', 'success');
        }
    });

    function sendToCloudAI(name, url) {
        if (!currentTranscription) return;
        navigator.clipboard.writeText(aiPromptPrefix + currentTranscription).then(() => window.open(url, '_blank'));
    }

    el('sendGemini').addEventListener('click', () => sendToCloudAI('Gemini', 'https://gemini.google.com/app'));
    el('sendClaude').addEventListener('click', () => sendToCloudAI('Claude', 'https://claude.ai/new'));
    el('sendChatGPT').addEventListener('click', () => sendToCloudAI('ChatGPT', 'https://chatgpt.com'));

    // --- Helpers ---
    function flashButton(btn, text, cls) {
        btn.classList.add(cls);
        const origHTML = btn.innerHTML;
        const svgEl = btn.querySelector('svg');
        btn.innerHTML = (svgEl ? svgEl.outerHTML : '') + ' ' + text;
        setTimeout(() => { btn.classList.remove(cls); btn.innerHTML = origHTML; }, 2000);
    }

})();
