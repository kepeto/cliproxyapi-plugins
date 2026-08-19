(function () {
  'use strict';

  const MGMT_STORAGE_KEY = 'cli-proxy-auth';
  const API_BASE = ''; // same origin

  const MODELS_URL = API_BASE + '/v0/management/plugins/playground/api/models';
  const AUTH_URL = API_BASE + '/v0/management/plugins/playground/api/auths';
  const CHAT_URL = API_BASE + '/v0/management/plugins/playground/api/chat';
  const CHAT_STREAM_URL = API_BASE + '/v0/management/plugins/playground/api/chat/stream';

  const authSelect = document.getElementById('auth-select');
  const modelSelect = document.getElementById('model-select');
  const streamSelect = document.getElementById('stream-select');
  const chatHistory = document.getElementById('chat-history');
  const chatForm = document.getElementById('chat-form');
  const chatInput = document.getElementById('chat-input');
  const sendButton = document.getElementById('send-button');
  const clearButton = document.getElementById('clear-button');
  const authTokenInput = document.getElementById('auth-token');

  function xorBytes(data, key) {
    var result = new Uint8Array(data.length);
    for (var i = 0; i < data.length; i++) {
      result[i] = data[i] ^ key[i % key.length];
    }
    return result;
  }

  function base64Decode(base64) {
    var binary = atob(base64);
    var bytes = new Uint8Array(binary.length);
    for (var i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
  }

  function base64Encode(bytes) {
    var binary = '';
    for (var i = 0; i < bytes.length; i++) {
      binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
  }

  function getKeyBytes() {
    var salt = 'cli-proxy-api-webui::secure-storage';
    try {
      var host = window.location.host;
      var ua = navigator.userAgent;
      var keyStr = salt + '|' + host + '|' + ua;
      var encoder = new TextEncoder();
      return encoder.encode(keyStr);
    } catch (e) {
      var fallback = new TextEncoder().encode(salt);
      return fallback;
    }
  }

  function deobfuscateData(payload) {
    if (!payload || typeof payload !== 'string') return '';
    var prefix = 'enc::v1::';
    if (!payload.startsWith(prefix)) return payload;
    try {
      var encodedBody = payload.slice(prefix.length);
      var encrypted = base64Decode(encodedBody);
      var keyBytes = getKeyBytes();
      var decrypted = xorBytes(encrypted, keyBytes);
      var decoder = new TextDecoder();
      return decoder.decode(decrypted);
    } catch (e) {
      return payload;
    }
  }

  function getManagementKey() {
    var inputToken = (authTokenInput && authTokenInput.value || '').trim();
    if (inputToken) return inputToken;
    try {
      var raw = localStorage.getItem(MGMT_STORAGE_KEY);
      if (!raw) return '';
      var decrypted = deobfuscateData(raw);
      var parsed = JSON.parse(decrypted);
      return parsed.managementKey || parsed.key || '';
    } catch (e) {
      return '';
    }
  }

  function managementHeaders() {
    var headers = { 'Content-Type': 'application/json' };
    var key = getManagementKey();
    if (key) {
      headers['Authorization'] = 'Bearer ' + key;
    }
    return headers;
  }

  async function loadModels() {
    try {
      var res = await fetch(MODELS_URL, { headers: managementHeaders() });
      if (!res.ok) throw new Error('Failed to load models: ' + res.status);
      var text = await res.text();
      var data;
      try { data = JSON.parse(text); } catch (e) { data = {}; }
      var models = [];
      if (Array.isArray(data)) {
        models = data.map(function(m) { return typeof m === 'string' ? m : m.id || m.name; });
      } else if (data.data && Array.isArray(data.data)) {
        models = data.data.map(function(m) { return m.id || m.name; });
      }
      modelSelect.innerHTML = '';
      if (models.length === 0) {
        modelSelect.innerHTML = '<option value="">No models found</option>';
        return;
      }
      models.forEach(function(id) {
        var opt = document.createElement('option');
        opt.value = id;
        opt.textContent = id;
        modelSelect.appendChild(opt);
      });
      if (models.length > 0) modelSelect.value = models[0];
    } catch (e) {
      modelSelect.innerHTML = '<option value="">Error loading models</option>';
      console.error(e);
    }
  }

  async function loadAuths() {
    try {
      var res = await fetch(AUTH_URL, { headers: managementHeaders() });
      if (!res.ok) throw new Error('Failed to load auths: ' + res.status);
      var text = await res.text();
      var data;
      try { data = JSON.parse(text); } catch (e) { data = {}; }
      var auths = [];
      if (Array.isArray(data)) {
        auths = data.map(function(a) { return a.auth_index || a.id || a.name; });
      } else if (data.files && Array.isArray(data.files)) {
        auths = data.files.map(function(a) { return a.auth_index || a.id || a.name; });
      } else if (data.data && Array.isArray(data.data)) {
        auths = data.data.map(function(a) { return a.auth_index || a.id || a.name; });
      }
      authSelect.innerHTML = '';
      if (auths.length === 0) {
        authSelect.innerHTML = '<option value="">No auths found</option>';
        return;
      }
      auths.forEach(function(id) {
        var opt = document.createElement('option');
        opt.value = id;
        opt.textContent = id;
        authSelect.appendChild(opt);
      });
      if (auths.length > 0) authSelect.value = auths[0];
    } catch (e) {
      authSelect.innerHTML = '<option value="">Error loading auths</option>';
      console.error(e);
    }
  }

  function appendMessage(role, content) {
    var bubble = document.createElement('div');
    bubble.className = 'message ' + role;
    var label = document.createElement('div');
    label.className = 'message-role';
    label.textContent = role === 'user' ? 'You' : 'Assistant';
    var text = document.createElement('div');
    text.className = 'message-content';
    text.textContent = content;
    bubble.appendChild(label);
    bubble.appendChild(text);
    chatHistory.appendChild(bubble);
    chatHistory.scrollTop = chatHistory.scrollHeight;
  }

  function appendStreamingMessage() {
    var bubble = document.createElement('div');
    bubble.className = 'message assistant streaming';
    var label = document.createElement('div');
    label.className = 'message-role';
    label.textContent = 'Assistant';
    var text = document.createElement('div');
    text.className = 'message-content';
    text.textContent = '';
    bubble.appendChild(label);
    bubble.appendChild(text);
    chatHistory.appendChild(bubble);
    chatHistory.scrollTop = chatHistory.scrollHeight;
    return text;
  }

  async function sendMessage(e) {
    e.preventDefault();
    var model = modelSelect.value;
    var stream = streamSelect.value === 'true';
    var content = chatInput.value.trim();
    if (!model || !content) return;

    var headers = managementHeaders();
    appendMessage('user', content);
    chatInput.value = '';
    sendButton.disabled = true;

    var body = JSON.stringify({
      model: model,
      messages: [{ role: 'user', content: content }],
      stream: stream
    });

    try {
      if (stream) {
        var res = await fetch(CHAT_STREAM_URL, {
          method: 'POST',
          headers: headers,
          body: body
        });
        if (!res.ok) throw new Error('Request failed: ' + res.status);

        var textEl = appendStreamingMessage();
        var reader = res.body.getReader();
        var decoder = new TextDecoder();
        var buffer = '';

        while (true) {
          var result = await reader.read();
          if (result.done) break;
          buffer += decoder.decode(result.value, { stream: true });
          var lines = buffer.split('\n');
          buffer = lines.pop() || '';
          for (var i = 0; i < lines.length; i++) {
            var line = lines[i].trim();
            if (!line || !line.startsWith('data:')) continue;
            var payload = line.slice(5).trim();
            if (payload === '[DONE]') continue;
            try {
              var chunk = JSON.parse(payload);
              var delta = chunk.choices && chunk.choices[0] && chunk.choices[0].delta;
              if (delta && delta.content) {
                textEl.textContent += delta.content;
                chatHistory.scrollTop = chatHistory.scrollHeight;
              }
            } catch (err) {
              // ignore parse errors
            }
          }
        }
      } else {
        var resp = await fetch(CHAT_URL, {
          method: 'POST',
          headers: headers,
          body: body
        });
        if (!resp.ok) throw new Error('Request failed: ' + resp.status);
        var data = JSON.parse(await resp.text());
        var choice = data.choices && data.choices[0];
        var content = choice && choice.message && choice.message.content;
        if (content) appendMessage('assistant', content);
      }
    } catch (err) {
      appendMessage('assistant', 'Error: ' + err.message);
      console.error(err);
    } finally {
      sendButton.disabled = false;
    }
  }

  chatForm.addEventListener('submit', sendMessage);
  clearButton.addEventListener('click', function() {
    chatHistory.innerHTML = '';
  });

  loadModels();
  loadAuths();
})();
