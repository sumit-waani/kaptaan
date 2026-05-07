package web

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width,initial-scale=1" />
<title>Kaptaan</title>
<script src="https://cdn.tailwindcss.com"></script>
<script defer src="https://unpkg.com/alpinejs@3.x.x/dist/cdn.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/dompurify@3.0.6/dist/purify.min.js"></script>
<style>
  body { font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
  .feed { scrollbar-width: thin; }
  .bubble { line-height: 1.55; }
  .bubble pre { background: #0b1220; color: #e5e7eb; padding: 8px 10px; border-radius: 6px; overflow-x: auto; font-size: 12px; }
  .bubble code { background: rgba(15,23,42,0.06); padding: 1px 4px; border-radius: 3px; font-size: 0.92em; }
  .bubble pre code { background: transparent; padding: 0; }
  .bubble h1, .bubble h2, .bubble h3 { font-weight: 600; margin: 6px 0 2px; }
  .bubble ul, .bubble ol { padding-left: 1.25rem; margin: 4px 0; }
  .bubble a { color: #2563eb; text-decoration: underline; }
  details > summary { cursor: pointer; }
  .pulse-dot { animation: pulse 1.4s infinite; }
  @keyframes pulse { 0%,100%{opacity:.4} 50%{opacity:1} }
</style>
</head>
<body class="bg-slate-50 text-slate-800 h-screen overflow-hidden">

<div x-data="kaptaan()" x-init="init()" x-cloak class="h-full flex flex-col">

  <!-- ── Auth gate ─────────────────────────────────────────────────────── -->
  <template x-if="!auth.loggedIn">
    <div class="h-full flex items-center justify-center bg-slate-100">
      <div class="bg-white rounded-2xl shadow p-8 w-96 space-y-4">
        <div class="flex items-center gap-2 mb-2">
          <div class="text-2xl">⚓</div>
          <h1 class="text-xl font-semibold">Kaptaan</h1>
        </div>
        <p class="text-sm text-slate-500" x-text="auth.hasUser ? 'Sign in to your account.' : 'Create your single user account.'"></p>
        <div class="space-y-2">
          <input x-model="auth.username" type="text" placeholder="username"
            class="w-full px-3 py-2 border border-slate-300 rounded-lg focus:ring-2 focus:ring-indigo-300 focus:outline-none">
          <input x-model="auth.password" type="password" placeholder="password (≥6 chars)"
            class="w-full px-3 py-2 border border-slate-300 rounded-lg focus:ring-2 focus:ring-indigo-300 focus:outline-none"
            @keyup.enter="auth.hasUser ? login() : signup()">
        </div>
        <template x-if="auth.error">
          <div class="text-sm text-red-600" x-text="auth.error"></div>
        </template>
        <button @click="auth.hasUser ? login() : signup()"
          class="w-full bg-indigo-600 hover:bg-indigo-700 text-white py-2 rounded-lg font-medium">
          <span x-text="auth.hasUser ? 'Sign in' : 'Create account'"></span>
        </button>
      </div>
    </div>
  </template>

  <!-- ── Main app ──────────────────────────────────────────────────────── -->
  <template x-if="auth.loggedIn">
    <div class="h-full flex flex-col">

      <!-- Header -->
      <header class="bg-white border-b border-slate-200 px-6 py-3 flex items-center gap-3">
        <div class="text-xl">⚓</div>
        <h1 class="text-lg font-semibold tracking-tight">Kaptaan</h1>
        <div class="text-xs text-slate-400">autonomous coding agent</div>
        <div class="ml-auto flex items-center gap-2">
          <div class="flex items-center gap-1 text-xs px-2 py-1 rounded-full"
               :class="agentRunning ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'">
            <span class="w-1.5 h-1.5 rounded-full"
                  :class="agentRunning ? 'bg-amber-500 pulse-dot' : 'bg-emerald-500'"></span>
            <span x-text="agentRunning ? 'working' : 'idle'"></span>
          </div>
          <button @click="showUsage = !showUsage"
            class="text-xs px-2 py-1 rounded text-slate-500 hover:bg-slate-100">usage</button>
          <button @click="showMemories = !showMemories"
            class="text-xs px-2 py-1 rounded text-slate-500 hover:bg-slate-100">memories</button>
          <button @click="showPlans = !showPlans"
            class="text-xs px-2 py-1 rounded text-slate-500 hover:bg-slate-100">plans</button>
          <button @click="showDocs = !showDocs"
            class="text-xs px-2 py-1 rounded text-slate-500 hover:bg-slate-100">docs</button>
          <button @click="logout()"
            class="text-xs px-2 py-1 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100">sign out</button>
        </div>
      </header>

      <!-- Project selector strip (just below header) -->
      <div class="bg-slate-100 border-b border-slate-200 px-6 py-2 flex items-center gap-3">
        <label class="text-xs font-medium text-slate-500 uppercase tracking-wide">Project</label>
        <div class="relative">
          <select x-model.number="activeProjectID" @change="onProjectChange()"
            class="text-sm pl-3 pr-8 py-1.5 rounded-lg border border-slate-300 bg-white focus:ring-2 focus:ring-indigo-300 focus:outline-none appearance-none">
            <template x-for="p in projects" :key="p.id">
              <option :value="p.id" x-text="p.name + (p.repo_url ? '  ·  ' + shortRepo(p.repo_url) : '')"></option>
            </template>
          </select>
          <div class="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-slate-400 text-xs">▾</div>
        </div>
        <template x-if="active()">
          <div class="flex items-center gap-2 text-xs text-slate-500">
            <template x-if="active().repo_url">
              <a :href="active().repo_url" target="_blank" class="hover:underline" x-text="active().repo_url"></a>
            </template>
            <template x-if="!active().repo_url">
              <span class="italic">no repo configured</span>
            </template>
            <template x-if="active().has_token">
              <span class="text-emerald-600">● token set</span>
            </template>
            <template x-if="active() && !active().has_token">
              <span class="text-amber-600">● no token</span>
            </template>
          </div>
        </template>
        <div class="ml-auto flex items-center gap-1">
          <button @click="openProjectEditor(active())"
            class="text-xs px-2 py-1 rounded text-slate-600 hover:bg-slate-200">edit</button>
          <button @click="openProjectEditor(null)"
            class="text-xs px-2 py-1 rounded bg-indigo-600 text-white hover:bg-indigo-700">+ new</button>
          <button @click="clearConvo()"
            class="text-xs px-2 py-1 rounded text-slate-500 hover:bg-slate-200">clear chat</button>
        </div>
      </div>

      <!-- Body: feed + composer -->
      <main class="flex-1 flex flex-col overflow-hidden">
        <div #feed class="feed flex-1 overflow-y-auto px-6 py-4 space-y-3" x-ref="feed">
          <template x-if="messages.length === 0">
            <div class="text-center text-slate-400 text-sm mt-20">
              <div class="text-3xl mb-2">⚓</div>
              <div>Send a message below to start working with Kaptaan on this project.</div>
            </div>
          </template>
          <template x-for="(m, i) in messages" :key="i">
            <div class="flex" :class="m.type === 'user' ? 'justify-end' : 'justify-start'">
              <div class="max-w-[820px] rounded-2xl px-4 py-2 shadow-sm bubble"
                   :class="bubbleClass(m)">
                <div class="text-[10px] uppercase tracking-wider opacity-60 mb-0.5"
                     x-text="bubbleLabel(m) + ' · ' + m.ts"></div>
                <div x-html="render(m.text)"></div>
              </div>
            </div>
          </template>
        </div>

        <!-- Composer -->
        <div class="border-t border-slate-200 bg-white px-6 py-3">
          <template x-if="askActive">
            <div class="text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded px-3 py-1.5 mb-2">
              ⏳ Kaptaan is waiting for your reply to the question above.
            </div>
          </template>
          <div class="flex gap-2">
            <textarea x-model="composer" rows="2" @keydown.meta.enter="send()" @keydown.ctrl.enter="send()"
              :placeholder="askActive ? 'Type your reply…' : 'Tell Kaptaan what to build…  (⌘/Ctrl+Enter)'"
              class="flex-1 resize-none rounded-lg border border-slate-300 px-3 py-2 text-sm focus:ring-2 focus:ring-indigo-300 focus:outline-none"></textarea>
            <button @click="send()" :disabled="!composer.trim()"
              class="px-4 py-2 rounded-lg bg-indigo-600 text-white text-sm font-medium hover:bg-indigo-700 disabled:opacity-50 disabled:cursor-not-allowed">
              Send
            </button>
          </div>
        </div>
      </main>
    </div>
  </template>

  <!-- ── Drawer: Project editor ────────────────────────────────────────── -->
  <template x-if="projectEditor.open">
    <div class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center" @click.self="projectEditor.open = false">
      <div class="bg-white rounded-2xl shadow-xl w-[480px] p-6 space-y-3">
        <h3 class="text-lg font-semibold" x-text="projectEditor.id ? 'Edit project' : 'New project'"></h3>
        <div>
          <label class="text-xs text-slate-500">Name</label>
          <input x-model="projectEditor.name" class="w-full px-3 py-2 border rounded-lg" placeholder="e.g. payments-api">
        </div>
        <div>
          <label class="text-xs text-slate-500">GitHub repo URL</label>
          <input x-model="projectEditor.repo_url" class="w-full px-3 py-2 border rounded-lg" placeholder="https://github.com/owner/repo">
        </div>
        <div>
          <label class="text-xs text-slate-500">GitHub token <span class="text-slate-400">(leave blank to keep existing)</span></label>
          <input x-model="projectEditor.github_token" type="password" class="w-full px-3 py-2 border rounded-lg" placeholder="ghp_…">
        </div>
        <template x-if="projectEditor.error">
          <div class="text-sm text-red-600" x-text="projectEditor.error"></div>
        </template>
        <div class="flex justify-between pt-2">
          <template x-if="projectEditor.id">
            <button @click="deleteProject(projectEditor.id)" class="text-sm text-red-600 hover:underline">Delete</button>
          </template>
          <div class="ml-auto flex gap-2">
            <button @click="projectEditor.open = false" class="px-3 py-1.5 rounded text-sm">Cancel</button>
            <button @click="saveProject()" class="px-3 py-1.5 rounded bg-indigo-600 text-white text-sm">Save</button>
          </div>
        </div>
      </div>
    </div>
  </template>

  <!-- ── Drawer: Usage ─────────────────────────────────────────────────── -->
  <template x-if="showUsage">
    <div class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center" @click.self="showUsage=false">
      <div class="bg-white rounded-2xl shadow-xl w-[560px] p-6">
        <div class="flex items-center justify-between mb-3">
          <h3 class="text-lg font-semibold">LLM usage</h3>
          <button @click="showUsage=false" class="text-slate-400 hover:text-slate-700">✕</button>
        </div>
        <div class="grid grid-cols-2 gap-4 text-sm">
          <div>
            <div class="text-xs uppercase text-slate-400 mb-1">Today</div>
            <template x-for="r in usage.today" :key="r.provider+r.model">
              <div class="flex justify-between border-b py-1">
                <span x-text="r.provider+' / '+r.model"></span>
                <span x-text="r.total_tokens.toLocaleString()+' tok'"></span>
              </div>
            </template>
            <template x-if="!usage.today || usage.today.length===0">
              <div class="text-slate-400 italic">no calls yet</div>
            </template>
          </div>
          <div>
            <div class="text-xs uppercase text-slate-400 mb-1">All-time</div>
            <template x-for="r in usage.all" :key="'a'+r.provider+r.model">
              <div class="flex justify-between border-b py-1">
                <span x-text="r.provider+' / '+r.model"></span>
                <span x-text="r.total_tokens.toLocaleString()+' tok'"></span>
              </div>
            </template>
          </div>
        </div>
      </div>
    </div>
  </template>

  <!-- ── Drawer: Memories / Plans / Docs ──────────────────────────────── -->
  <template x-if="showMemories">
    <div class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center" @click.self="showMemories=false">
      <div class="bg-white rounded-2xl shadow-xl w-[640px] max-h-[80vh] overflow-y-auto p-6 space-y-3">
        <div class="flex items-center justify-between">
          <h3 class="text-lg font-semibold">Memories</h3>
          <button @click="showMemories=false" class="text-slate-400 hover:text-slate-700">✕</button>
        </div>
        <template x-for="m in memories" :key="m.key">
          <div class="border rounded-lg p-3">
            <div class="flex items-center justify-between mb-1">
              <code class="text-sm font-medium" x-text="m.key"></code>
              <button @click="deleteMemory(m.key)" class="text-xs text-red-500 hover:underline">delete</button>
            </div>
            <div class="text-sm text-slate-600 whitespace-pre-wrap" x-text="m.content"></div>
            <div class="text-[10px] text-slate-400 mt-1" x-text="m.updated_at"></div>
          </div>
        </template>
        <template x-if="memories.length === 0">
          <div class="text-sm text-slate-400 italic">No memories saved for this project yet.</div>
        </template>
      </div>
    </div>
  </template>

  <template x-if="showPlans">
    <div class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center" @click.self="showPlans=false">
      <div class="bg-white rounded-2xl shadow-xl w-[720px] max-h-[80vh] overflow-y-auto p-6 space-y-3">
        <div class="flex items-center justify-between">
          <h3 class="text-lg font-semibold">Plans</h3>
          <button @click="showPlans=false" class="text-slate-400 hover:text-slate-700">✕</button>
        </div>
        <template x-for="p in plans" :key="p.filename">
          <div class="border rounded-lg p-3">
            <div class="flex items-center justify-between mb-1">
              <code class="text-sm" x-text="p.filename"></code>
              <button @click="loadPlan(p.filename)" class="text-xs text-indigo-600 hover:underline">view</button>
            </div>
            <div class="text-[10px] text-slate-400" x-text="p.created+' · '+p.bytes+' B'"></div>
            <template x-if="planView.filename === p.filename">
              <pre class="mt-2 text-xs bg-slate-50 p-2 rounded whitespace-pre-wrap" x-text="planView.content"></pre>
            </template>
          </div>
        </template>
        <template x-if="plans.length === 0">
          <div class="text-sm text-slate-400 italic">No plans written for this project yet.</div>
        </template>
      </div>
    </div>
  </template>

  <template x-if="showDocs">
    <div class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center" @click.self="showDocs=false">
      <div class="bg-white rounded-2xl shadow-xl w-[560px] max-h-[80vh] overflow-y-auto p-6 space-y-3">
        <div class="flex items-center justify-between">
          <h3 class="text-lg font-semibold">Reference docs</h3>
          <button @click="showDocs=false" class="text-slate-400 hover:text-slate-700">✕</button>
        </div>
        <input type="file" @change="uploadDoc($event)" class="text-sm">
        <template x-for="d in docs" :key="d.id">
          <div class="border rounded-lg p-2 flex items-center justify-between">
            <div>
              <div class="text-sm font-medium" x-text="d.filename"></div>
              <div class="text-[10px] text-slate-400" x-text="d.created+' · '+d.bytes+' B'"></div>
            </div>
            <button @click="deleteDoc(d.id)" class="text-xs text-red-500 hover:underline">delete</button>
          </div>
        </template>
        <template x-if="docs.length === 0">
          <div class="text-sm text-slate-400 italic">No docs uploaded for this project yet.</div>
        </template>
      </div>
    </div>
  </template>

</div>

<script>
function kaptaan() {
  return {
    auth: { loggedIn:false, hasUser:false, username:'', password:'', error:'' },
    projects: [],
    activeProjectID: null,
    messages: [],
    composer: '',
    agentRunning: false,
    askActive: false,
    sse: null,
    showUsage:false, showMemories:false, showPlans:false, showDocs:false,
    usage: { all:[], today:[] },
    memories: [], plans: [], docs: [],
    planView: { filename:'', content:'' },
    projectEditor: { open:false, id:0, name:'', repo_url:'', github_token:'', error:'' },

    async init() {
      const s = await fetch('/api/auth/status').then(r=>r.json());
      this.auth.hasUser  = s.hasUser;
      this.auth.loggedIn = s.loggedIn;
      if (this.auth.loggedIn) await this.bootApp();
    },
    async signup() {
      this.auth.error='';
      const r = await fetch('/api/auth/setup',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:this.auth.username,password:this.auth.password})});
      if (!r.ok) { this.auth.error = (await r.json()).error || 'failed'; return; }
      this.auth.loggedIn = true; this.auth.hasUser = true; await this.bootApp();
    },
    async login() {
      this.auth.error='';
      const r = await fetch('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:this.auth.username,password:this.auth.password})});
      if (!r.ok) { this.auth.error = (await r.json()).error || 'invalid credentials'; return; }
      this.auth.loggedIn = true; await this.bootApp();
    },
    async logout() {
      await fetch('/api/auth/logout',{method:'POST'});
      this.auth.loggedIn = false; if (this.sse) this.sse.close();
    },
    async bootApp() {
      await this.loadProjects();
      const stored = parseInt(localStorage.getItem('kaptaan_active_project') || '0');
      if (stored && this.projects.some(p=>p.id===stored)) this.activeProjectID = stored;
      else if (this.projects.length) this.activeProjectID = this.projects[0].id;
      else { this.openProjectEditor(null); return; }
      this.connect();
      this.refreshSidePanels();
    },
    async loadProjects() {
      const j = await this.api('/api/projects');
      this.projects = j.projects || [];
    },
    active() { return this.projects.find(p => p.id === this.activeProjectID); },
    shortRepo(u) { return u.replace(/^https?:\/\/(www\.)?github\.com\//,''); },
    onProjectChange() {
      localStorage.setItem('kaptaan_active_project', String(this.activeProjectID));
      this.messages = []; this.askActive = false;
      this.connect(); this.refreshSidePanels();
    },
    async refreshSidePanels() {
      this.refreshUsage(); this.refreshMemories(); this.refreshPlans(); this.refreshDocs();
    },
    connect() {
      if (this.sse) this.sse.close();
      this.sse = new EventSource('/events?project='+this.activeProjectID);
      this.sse.addEventListener('msg', e => this.onMsg(JSON.parse(e.data)));
      this.sse.addEventListener('state', e => { this.agentRunning = JSON.parse(e.data).running; });
      this.sse.addEventListener('ask_state', e => { this.askActive = JSON.parse(e.data).active; });
    },
    onMsg(m) {
      this.messages.push(m);
      if (m.type === 'ask') this.askActive = true;
      if (m.type === 'reply') this.askActive = false;
      this.$nextTick(() => {
        const f = this.$refs.feed;
        if (f) f.scrollTop = f.scrollHeight;
      });
    },
    bubbleClass(m) {
      switch (m.type) {
        case 'user':  return 'bg-indigo-600 text-white';
        case 'reply': return 'bg-indigo-100 text-indigo-900';
        case 'ask':   return 'bg-amber-100 text-amber-900 border border-amber-200';
        default:      return 'bg-white text-slate-800 border border-slate-200';
      }
    },
    bubbleLabel(m) {
      return ({user:'you', reply:'you', ask:'kaptaan asks', message:'kaptaan'}[m.type]) || 'kaptaan';
    },
    render(text) {
      try { return DOMPurify.sanitize(marked.parse(text || '', {breaks:true})); }
      catch(e) { return DOMPurify.sanitize(text||''); }
    },
    async send() {
      const text = this.composer.trim();
      if (!text) return;
      this.composer = '';
      const path = this.askActive ? '/api/reply' : '/api/chat';
      const r = await this.api(path, { method:'POST', body: JSON.stringify({text}) });
      if (r.error) this.onMsg({type:'message', text:'❌ '+r.error, ts:new Date().toLocaleTimeString()});
    },
    async clearConvo() {
      await this.api('/api/conversation/clear', {method:'POST'});
      this.messages = [];
    },
    openProjectEditor(p) {
      this.projectEditor = p
        ? { open:true, id:p.id, name:p.name, repo_url:p.repo_url, github_token:'', error:'' }
        : { open:true, id:0, name:'', repo_url:'', github_token:'', error:'' };
    },
    async saveProject() {
      this.projectEditor.error = '';
      const body = { name: this.projectEditor.name, repo_url: this.projectEditor.repo_url };
      if (this.projectEditor.github_token) body.github_token = this.projectEditor.github_token;
      const url = this.projectEditor.id ? '/api/projects/'+this.projectEditor.id : '/api/projects';
      const method = this.projectEditor.id ? 'PATCH' : 'POST';
      const r = await fetch(url, {method, headers:{'Content-Type':'application/json'}, body: JSON.stringify(body)});
      const j = await r.json();
      if (!r.ok) { this.projectEditor.error = j.error || 'failed'; return; }
      await this.loadProjects();
      if (j.project) this.activeProjectID = j.project.id;
      localStorage.setItem('kaptaan_active_project', String(this.activeProjectID));
      this.projectEditor.open = false;
      this.connect(); this.refreshSidePanels();
    },
    async deleteProject(id) {
      if (!confirm('Delete this project? Conversation, plan files and memories will be lost.')) return;
      const r = await fetch('/api/projects/'+id, {method:'DELETE'});
      const j = await r.json();
      if (!r.ok) { this.projectEditor.error = j.error || 'failed'; return; }
      this.projectEditor.open = false;
      this.activeProjectID = null;
      await this.loadProjects();
      if (this.projects.length) {
        this.activeProjectID = this.projects[0].id;
        localStorage.setItem('kaptaan_active_project', String(this.activeProjectID));
        this.connect(); this.refreshSidePanels();
      } else {
        this.openProjectEditor(null);
      }
    },
    async refreshUsage() { this.usage = await fetch('/api/usage').then(r=>r.json()); },
    async refreshMemories() { const j = await this.api('/api/memories'); this.memories = j.memories || []; },
    async refreshPlans() { const j = await this.api('/api/plans'); this.plans = j.plans || []; },
    async refreshDocs() { const j = await this.api('/api/docs'); this.docs = j.docs || []; },
    async deleteMemory(key) {
      await this.api('/api/memories?key='+encodeURIComponent(key), {method:'DELETE'});
      this.refreshMemories();
    },
    async loadPlan(file) {
      const j = await this.api('/api/plans?file='+encodeURIComponent(file));
      this.planView = { filename: file, content: j.content || '' };
    },
    async uploadDoc(ev) {
      const f = ev.target.files[0]; if (!f) return;
      const fd = new FormData(); fd.append('file', f);
      await fetch('/api/docs', { method:'POST', headers: {'X-Project-ID': String(this.activeProjectID)}, body: fd });
      this.refreshDocs();
    },
    async deleteDoc(id) {
      await this.api('/api/docs/'+id, {method:'DELETE'});
      this.refreshDocs();
    },
    async api(path, opts={}) {
      const headers = Object.assign({'Content-Type':'application/json','X-Project-ID': String(this.activeProjectID||'')}, opts.headers||{});
      const r = await fetch(path, Object.assign({}, opts, {headers}));
      try { return await r.json(); } catch(e) { return {}; }
    },
  };
}
</script>
</body>
</html>
`
