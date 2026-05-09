function kaptaan() {
  return {
    auth: { loggedIn:false, hasUser:false, username:'', password:'', error:'' },
    messages: [],
    composer: '',
    agentRunning: false,
    cancellingTask: false,
    askActive: false,
    hasQueued: false,
    sse: null,
    showSettings: false,
    memories: [],
    credits: { loading: false, error: '', data: null },
    scratchpad: { loading: false, error: '', content: null },
    cfg: {
      values: { deepseek_api_key:'', deepseek_model:'', e2b_api_key:'', repo_url:'', github_token:'', system_prompt:'' },
      show: { deepseek_api_key:false, e2b_api_key:false, github_token:false },
      saving: false, saved: false, error: '',
    },
    toolGroupsOpen: {},
    lastToolName: '',

    // streaming state
    streamingText: '',
    isStreaming: false,

    async init() {
      const s = await fetch('/api/auth/status').then(r=>r.json());
      this.auth.hasUser  = s.hasUser;
      this.auth.loggedIn = s.loggedIn;
      if (this.auth.loggedIn) this.bootApp();
    },

    async signup() {
      this.auth.error = '';
      const r = await fetch('/api/auth/setup', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({username:this.auth.username, password:this.auth.password})});
      if (!r.ok) { this.auth.error = (await r.json()).error||'failed'; return; }
      this.auth.loggedIn = true; this.auth.hasUser = true; this.bootApp();
    },

    async login() {
      this.auth.error = '';
      const r = await fetch('/api/auth/login', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({username:this.auth.username, password:this.auth.password})});
      if (!r.ok) { this.auth.error = (await r.json()).error||'invalid credentials'; return; }
      this.auth.loggedIn = true; this.bootApp();
    },

    async logout() {
      await fetch('/api/auth/logout', {method:'POST'});
      this.auth.loggedIn = false; this.showSettings = false;
      if (this.sse) this.sse.close();
    },

    bootApp() {
      this.connect();
    },

    connect() {
      if (this.sse) this.sse.close();
      this.sse = new EventSource('/events');
      this.sse.addEventListener('error', () => {
        // On persistent SSE error, verify session is still valid.
        fetch('/api/auth/status').then(r => r.json()).then(s => {
          if (!s.loggedIn) {
            this.auth.loggedIn = false;
            this.showSettings = false;
            if (this.sse) { this.sse.close(); this.sse = null; }
          }
        }).catch(()=>{});
      });
      this.sse.addEventListener('msg', e => this.onMsg(JSON.parse(e.data)));
      this.sse.addEventListener('state', e => {
        const s = JSON.parse(e.data);
        this.agentRunning = s.running;
        this.hasQueued = s.queued || false;
        if (!s.running) { this.lastToolName = ''; this.cancellingTask = false; }
      });
      this.sse.addEventListener('ask_state', e => { this.askActive = JSON.parse(e.data).active; });
      this.sse.addEventListener('token', e => {
        const d = JSON.parse(e.data);
        this.isStreaming = true;
        this.streamingText += d.text;
        this._scrollFeed();
      });
      this.sse.addEventListener('stream_cancel', () => {
        this.streamingText = '';
        this.isStreaming = false;
      });
      this.sse.addEventListener('stream_done', () => {
        if (this.streamingText) {
          this.messages.push({
            type: 'message',
            text: this.streamingText,
            ts: new Date().toTimeString().slice(0,8),
          });
        }
        this.streamingText = '';
        this.isStreaming = false;
        this._scrollFeed();
      });
    },

    _scrollFeed() {
      this.$nextTick(() => {
        const f = this.$refs.feed;
        if (f) f.scrollTop = f.scrollHeight;
      });
    },

    onMsg(m) {
      // A tool call message cancels any active streaming buffer (intermediate step)
      if (this.isToolCall(m)) {
        this.streamingText = '';
        this.isStreaming = false;
      }
      this.messages.push(m);
      if (this.isToolCall(m)) {
        const parsed = this.parseToolCall(m.text);
        if (parsed.name) this.lastToolName = parsed.name;
      }
      this._scrollFeed();
    },

    bubbleClass(m) {
      if (m.type === 'user')  return 'bubble user-bubble';
      if (m.type === 'reply') return 'bubble reply-bubble';
      if (m.type === 'ask')   return 'bubble ask-bubble';
      return 'bubble agent-bubble';
    },

    bubbleLabel(m) {
      return ({user:'you', reply:'you', ask:'kaptaan asks', message:'kaptaan'}[m.type])||'kaptaan';
    },

    isToolCall(m) {
      if (m.type !== 'message' || typeof m.text !== 'string') return false;
      const tick = String.fromCharCode(96);
      return m.text.startsWith(tick + 'tool' + tick);
    },

    groupedMessages() {
      const groups = [];
      let i = 0;
      while (i < this.messages.length) {
        const m = this.messages[i];
        if (this.isToolCall(m)) {
          const startIdx = i;
          const tools = [];
          while (i < this.messages.length && this.isToolCall(this.messages[i])) {
            tools.push(this.messages[i]);
            i++;
          }
          groups.push({ kind: 'toolgroup', tools, gid: 'tg' + startIdx });
        } else {
          groups.push({ kind: 'message', msg: m, gid: 'msg' + i });
          i++;
        }
      }
      return groups;
    },

    parseToolCall(text) {
      const m = text.match(/^\x60tool\x60\s+\*\*([^*]+)\*\*\s*([\s\S]*)/);
      if (m) return { name: m[1].trim(), args: m[2].trim() };
      return { name: text, args: '' };
    },

    isToolGroupOpen(gid) {
      return this.toolGroupsOpen[gid] !== false;
    },

    isLastToolGroup(gid) {
      const groups = this.groupedMessages();
      const toolGroups = groups.filter(g => g.kind === 'toolgroup');
      return toolGroups.length > 0 && toolGroups[toolGroups.length - 1].gid === gid;
    },

    toggleToolGroup(gid) {
      this.toolGroupsOpen[gid] = !this.isToolGroupOpen(gid);
    },

    render(text) {
      try { return DOMPurify.sanitize(marked.parse(text||'', {breaks:true})); }
      catch(e) { return DOMPurify.sanitize(text||''); }
    },

    async send() {
      const text = this.composer.trim();
      if (!text) return;
      this.composer = '';
      this.$nextTick(() => {
        const ta = document.querySelector('.composer-input');
        if (ta) { ta.style.height = ''; }
      });
      const path = this.askActive ? '/api/reply' : '/api/chat';
      const r = await this.api(path, {method:'POST', body: JSON.stringify({text})});
      if (r.error) this.onMsg({type:'message', text:'error: '+r.error, ts: new Date().toTimeString().slice(0,8)});
    },

    autoResize(ev) {
      const el = ev.target;
      el.style.height = '';
      el.style.height = Math.min(el.scrollHeight, 120) + 'px';
    },

    async cancelTask() {
      if (this.cancellingTask) return;
      this.cancellingTask = true;
      await this.api('/api/task/cancel', {method:'POST'});
    },

    async clearConvo() {
      await this.api('/api/conversation/clear', {method:'POST'});
      this.messages = [];
    },

    openSettings() {
      this.showSettings = true;
      this.refreshSettings();
      this.loadScratchpad();
      this.loadConfig();
    },

    closeSettings() {
      this.showSettings = false;
    },

    async refreshSettings() {
      const j = await this.api('/api/memories');
      this.memories = j.memories||[];
    },

    async deleteMemory(key) {
      await this.api('/api/memories?key='+encodeURIComponent(key), {method:'DELETE'});
      this.memories = this.memories.filter(m=>m.key!==key);
    },

    async loadScratchpad() {
      this.scratchpad = { loading: true, error: '', content: null };
      const j = await this.api('/api/scratchpad');
      if (j.error) {
        this.scratchpad = { loading: false, error: j.error, content: null };
        return;
      }
      this.scratchpad = { loading: false, error: '', content: j.content };
    },

    async loadConfig() {
      const j = await this.api('/api/config');
      if (j.config) {
        this.cfg.values = Object.assign(this.cfg.values, j.config);
      }
    },

    async saveConfig() {
      this.cfg.saving = true;
      this.cfg.error = '';
      for (const [key, value] of Object.entries(this.cfg.values)) {
        const r = await this.api('/api/config', {method:'POST', body: JSON.stringify({key, value})});
        if (r.error) {
          this.cfg.error = r.error;
          this.cfg.saving = false;
          return;
        }
      }
      this.cfg.saving = false;
      this.cfg.saved = true;
      setTimeout(() => { this.cfg.saved = false; }, 2500);
    },

    async checkCredits() {
      this.credits = { loading: true, error: '', data: null };
      const j = await fetch('/api/credits').then(r=>r.json()).catch(()=>({error:'network error'}));
      if (j.error) {
        this.credits = { loading: false, error: j.error, data: null };
        return;
      }
      this.credits = { loading: false, error: '', data: j };
    },

    api(path, opts={}) {
      const headers = Object.assign({'Content-Type':'application/json'}, opts.headers||{});
      return fetch(path, Object.assign({}, opts, {headers})).then(r => {
        if (r.status === 401) {
          this.auth.loggedIn = false;
          this.showSettings = false;
          if (this.sse) { this.sse.close(); this.sse = null; }
          return {error: 'session expired'};
        }
        return r.json();
      }).catch(()=>({}));
    },
  };
}
