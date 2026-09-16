/**
 * RemGo — Lightweight, Zero-Dependency Outliner & Spaced Repetition Engine
 */

class RemGoApp {
  constructor() {
    this.activeView = 'outliner';
    this.currentDocID = null;
    this.documents = [];
    this.currentTree = [];
    this.currentRem = null;
    this.breadcrumbs = [];
    this.dueCards = [];
    this.currentCardIndex = 0;
    this.isAnswerRevealed = false;
    this.isCramMode = false;
    this.theme = localStorage.getItem('remgo_theme') || 'dark';
    this.searchSelectedIndex = 0;
    this.searchResults = [];

    // Authentication State
    this.authEnabled = false;
    this.hasUsers = false;
    this.currentUser = null;
    this.authMode = 'login'; // 'login', 'register', 'setup'
    this.token = localStorage.getItem('remgo_token') || '';

    // Graph physics simulation
    this.graphData = null;
    this.graphNodes = [];
    this.graphEdges = [];
    this.graphAnimId = null;
    this.dragNode = null;

    // PDF Reader State
    this.pdfDocuments = [];
    this.currentPdfDoc = null;
    this.currentPdf = null;
    this.currentPdfPage = 1;
    this.pdfScale = 1.0;
    this.pdfHighlights = [];
    this.activePdfSelection = null;
    this.activeHighlightColor = '#ffeb3b';
    this.renderedPdfPages = new Set();
    this.renderingPdfPages = new Set();
    this.isPdfSplitActive = false;
    this.isPdfFullscreen = false;
    this.lastFocusedRemID = null;
    this.lastFocusedParentID = null;

    this.init();
  }

  // Central authenticated fetch wrapper
  async fetchAPI(url, options = {}) {
    const headers = options.headers ? { ...options.headers } : {};
    if (this.token) {
      headers['Authorization'] = 'Bearer ' + this.token;
    }

    const mergedOptions = {
      ...options,
      headers
    };

    try {
      const res = await fetch(url, mergedOptions);
      if (res.status === 401 && this.authEnabled) {
        this.handleUnauthorized();
        return null;
      }
      return res;
    } catch (e) {
      console.error('Fetch error:', url, e);
      throw e;
    }
  }

  handleUnauthorized() {
    this.token = '';
    localStorage.removeItem('remgo_token');
    this.currentUser = null;
    this.updateUserProfileUI();
    this.openAuthModal(this.hasUsers ? 'login' : 'setup');
  }

  async init() {
    this.applyTheme(this.theme);
    this.bindGlobalShortcuts();
    this.initSplitResizer();
    this.initPdfTextSelectionListener();

    // Check Auth Status
    await this.checkAuthStatus();

    // If auth is required and user is not authenticated, stop here until login
    if (this.authEnabled && !this.currentUser) {
      return;
    }

    await this.loadDocuments();
    await this.refreshDueBadge();
    this.loadPdfLibrary();

    // If documents exist, load first document by default
    if (this.documents.length > 0) {
      await this.zoomTo(this.documents[0].id);
    } else {
      await this.createNewDoc('Welcome to RemGo');
    }
  }

  // Auth Status & User Profile
  async checkAuthStatus() {
    try {
      const res = await this.fetchAPI('/api/auth/status');
      if (!res || !res.ok) return;
      const status = await res.json();
      this.authEnabled = status.auth_enabled;
      this.hasUsers = status.has_users;

      if (status.authenticated && status.user) {
        this.currentUser = status.user;
      } else if (!this.authEnabled) {
        this.currentUser = status.user || { username: 'default', role: 'local' };
      } else {
        this.currentUser = null;
        this.token = '';
        localStorage.removeItem('remgo_token');
      }

      this.updateUserProfileUI();

      // If auth is enabled but not authenticated, trigger appropriate modal
      if (this.authEnabled && !this.currentUser) {
        if (!this.hasUsers) {
          this.openAuthModal('setup');
        } else {
          this.openAuthModal('login');
        }
      }
    } catch (e) {
      console.error('Failed to check auth status', e);
    }
  }

  updateUserProfileUI() {
    const avatar = document.getElementById('userAvatar');
    const name = document.getElementById('userName');
    const role = document.getElementById('userRole');
    const actionBtn = document.getElementById('authActionBtn');

    if (this.currentUser) {
      const uname = this.currentUser.username || 'User';
      avatar.innerText = uname.charAt(0).toUpperCase();
      name.innerText = uname;
      role.innerText = this.currentUser.role ? (this.currentUser.role.charAt(0).toUpperCase() + this.currentUser.role.slice(1)) : 'Local';
      actionBtn.title = 'Log Out';
      actionBtn.innerHTML = '🚪';
    } else {
      avatar.innerText = '?';
      name.innerText = 'Not signed in';
      role.innerText = 'Guest';
      actionBtn.title = 'Log In';
      actionBtn.innerHTML = '🔑';
    }
  }

  openAuthOrProfile() {
    if (this.currentUser && this.authEnabled) {
      this.openAPIKeysModal();
    } else {
      this.openAuthModal(this.hasUsers ? 'login' : 'setup');
    }
  }

  handleAuthActionClick() {
    if (this.currentUser && this.authEnabled) {
      this.logout();
    } else {
      this.openAuthModal(this.hasUsers ? 'login' : 'setup');
    }
  }

  // Auth Modal Management
  openAuthModal(mode = 'login') {
    this.authMode = mode;
    const modal = document.getElementById('authModal');
    const tabs = document.getElementById('authTabs');
    const title = document.getElementById('authModalTitle');
    const subtitle = document.getElementById('authModalSubtitle');
    const submitBtn = document.getElementById('authSubmitBtn');
    const emailGroup = document.getElementById('authEmailGroup');
    const footer = document.getElementById('authFooter');
    const alertBox = document.getElementById('authAlert');

    alertBox.classList.add('hidden');
    alertBox.innerText = '';

    if (mode === 'setup') {
      title.innerText = 'Create Admin Account';
      subtitle.innerText = 'Welcome to RemGo! Setup your first administrator account.';
      tabs.style.display = 'none';
      emailGroup.style.display = 'block';
      submitBtn.innerText = 'Create Admin Account';
      footer.style.display = 'none';
    } else if (mode === 'register') {
      title.innerText = 'Create an Account';
      subtitle.innerText = 'Sign up to start organizing notes and flashcards.';
      tabs.style.display = 'flex';
      emailGroup.style.display = 'block';
      submitBtn.innerText = 'Sign Up';
      footer.style.display = 'block';
      document.getElementById('authTabLogin').classList.remove('active');
      document.getElementById('authTabRegister').classList.add('active');
      document.getElementById('authSwitchPrompt').innerText = 'Already have an account?';
      document.getElementById('authSwitchLink').innerText = 'Log In';
    } else {
      // login
      title.innerText = 'Welcome to RemGo';
      subtitle.innerText = 'Sign in to access your notes and review schedules.';
      tabs.style.display = 'flex';
      emailGroup.style.display = 'none';
      submitBtn.innerText = 'Log In';
      footer.style.display = 'block';
      document.getElementById('authTabLogin').classList.add('active');
      document.getElementById('authTabRegister').classList.remove('active');
      document.getElementById('authSwitchPrompt').innerText = "Don't have an account?";
      document.getElementById('authSwitchLink').innerText = 'Create one';
    }

    modal.classList.remove('hidden');
    document.getElementById('authUsername').focus();
  }

  switchAuthTab(tab) {
    this.openAuthModal(tab);
  }

  toggleAuthMode() {
    this.openAuthModal(this.authMode === 'login' ? 'register' : 'login');
  }

  async submitAuth(event) {
    event.preventDefault();
    const alertBox = document.getElementById('authAlert');
    alertBox.classList.add('hidden');
    alertBox.innerText = '';

    const username = document.getElementById('authUsername').value.trim();
    const email = document.getElementById('authEmail').value.trim();
    const password = document.getElementById('authPassword').value;

    let endpoint = '/api/auth/login';
    let body = { username, password };

    if (this.authMode === 'setup') {
      endpoint = '/api/auth/setup';
      body = { username, email: email || `${username}@remgo.local`, password };
    } else if (this.authMode === 'register') {
      endpoint = '/api/auth/register';
      body = { username, email: email || `${username}@remgo.local`, password };
    }

    try {
      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      });

      const data = await res.json().catch(() => ({ error: res.statusText || 'Authentication failed' }));
      if (!res.ok) {
        alertBox.innerText = data.error || 'Authentication failed';
        alertBox.classList.remove('hidden');
        return;
      }

      // Success!
      this.token = data.token;
      localStorage.setItem('remgo_token', data.token);
      this.currentUser = data.user;
      this.hasUsers = true;
      this.updateUserProfileUI();
      this.closeModal('authModal');

      // Refresh application data
      await this.loadDocuments();
      await this.refreshDueBadge();
      if (this.documents.length > 0) {
        await this.zoomTo(this.documents[0].id);
      } else {
        await this.createNewDoc('Welcome to RemGo');
      }
    } catch (e) {
      alertBox.innerText = 'Network error: ' + e.message;
      alertBox.classList.remove('hidden');
    }
  }

  async logout() {
    try {
      await this.fetchAPI('/api/auth/logout', { method: 'POST' });
    } catch (e) {
      console.error('Logout error', e);
    }
    this.token = '';
    localStorage.removeItem('remgo_token');
    this.currentUser = null;
    this.documents = [];
    this.currentTree = [];
    this.currentRem = null;
    this.updateUserProfileUI();
    this.renderDocsList();
    this.renderOutlinerTree([]);

    const dueBadge = document.getElementById('dueBadge');
    if (dueBadge) dueBadge.innerText = '0';
    const breadcrumbs = document.getElementById('breadcrumbsBar');
    if (breadcrumbs) breadcrumbs.innerHTML = '<span class="breadcrumb-item" onclick="app.zoomTo(null)">Home</span>';
    const docTitle = document.getElementById('docTitleInput');
    if (docTitle) docTitle.value = '';

    if (this.authEnabled) {
      this.openAuthModal('login');
    } else {
      await this.checkAuthStatus();
      await this.loadDocuments();
    }
  }

  // Personal Access Tokens (API Keys) Modal
  async openAPIKeysModal() {
    const modal = document.getElementById('apiKeysModal');
    document.getElementById('newKeyDisplayBox').classList.add('hidden');
    document.getElementById('newKeyNameInput').value = '';
    modal.classList.remove('hidden');
    await this.loadAPIKeys();
  }

  async loadAPIKeys() {
    const list = document.getElementById('keysList');
    list.innerHTML = '<div style="padding:12px;font-size:0.85rem;color:var(--text-muted);">Loading tokens...</div>';

    try {
      const res = await this.fetchAPI('/api/auth/keys');
      if (!res || !res.ok) {
        list.innerHTML = '<div style="padding:12px;font-size:0.85rem;color:var(--again-color);">Failed to load tokens</div>';
        return;
      }
      const keys = await res.json();
      if (!keys || keys.length === 0) {
        list.innerHTML = '<div style="padding:12px;font-size:0.85rem;color:var(--text-muted);">No active tokens created yet.</div>';
        return;
      }

      list.innerHTML = '';
      keys.forEach(k => {
        const item = document.createElement('div');
        item.className = 'key-item';
        const createdDate = new Date(k.created_at).toLocaleDateString();
        const lastUsed = k.last_used_at ? new Date(k.last_used_at).toLocaleDateString() : 'Never';
        item.innerHTML = `
          <div class="key-item-info">
            <span class="key-name">${this.escapeHTML(k.name)}</span>
            <span class="key-prefix"><code>${this.escapeHTML(k.key_prefix)}</code></span>
            <span class="key-meta">Created: ${createdDate} • Last used: ${lastUsed}</span>
          </div>
          <button class="btn btn-sm btn-danger" onclick="app.deleteAPIKey('${k.id}')">Revoke</button>
        `;
        list.appendChild(item);
      });
    } catch (e) {
      console.error('Failed to load API keys', e);
    }
  }

  async createAPIKey() {
    const input = document.getElementById('newKeyNameInput');
    const name = input.value.trim() || 'MCP Agent Token';

    try {
      const res = await this.fetchAPI('/api/auth/keys', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name })
      });

      if (!res || !res.ok) {
        alert('Failed to generate token');
        return;
      }

      const data = await res.json();
      input.value = '';

      // Display newly created token
      const displayBox = document.getElementById('newKeyDisplayBox');
      const textEl = document.getElementById('newKeyText');
      textEl.innerText = data.key;
      this.lastGeneratedKey = data.key;
      displayBox.classList.remove('hidden');

      await this.loadAPIKeys();
    } catch (e) {
      alert('Error creating API key: ' + e.message);
    }
  }

  async deleteAPIKey(keyID) {
    if (!confirm('Are you sure you want to revoke this Personal Access Token? Any MCP agent using it will lose access.')) {
      return;
    }

    try {
      const res = await this.fetchAPI(`/api/auth/keys/${keyID}`, { method: 'DELETE' });
      if (res && res.ok) {
        await this.loadAPIKeys();
      }
    } catch (e) {
      alert('Failed to revoke key: ' + e.message);
    }
  }

  copyNewKey() {
    if (this.lastGeneratedKey) {
      navigator.clipboard.writeText(this.lastGeneratedKey);
      alert('Token copied to clipboard!');
    }
  }

  // Theme Management
  applyTheme(theme) {
    this.theme = theme;
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('remgo_theme', theme);
  }

  toggleTheme() {
    this.applyTheme(this.theme === 'dark' ? 'light' : 'dark');
  }

  // View Navigation
  switchView(view) {
    this.activeView = view;
    const splitWrapper = document.getElementById('outlinerSplitWrapper');
    if (splitWrapper) {
      splitWrapper.style.display = (view === 'outliner') ? 'flex' : 'none';
    }
    document.getElementById('reviewerView').style.display = view === 'reviewer' ? 'block' : 'none';
    document.getElementById('graphView').style.display = view === 'graph' ? 'block' : 'none';
    const pdfLibView = document.getElementById('pdfLibraryView');
    if (pdfLibView) {
      pdfLibView.style.display = view === 'pdfs' ? 'block' : 'none';
    }

    document.querySelectorAll('.nav-item').forEach(el => el.classList.remove('active'));
    if (view === 'outliner') document.getElementById('navOutliner')?.classList.add('active');
    if (view === 'reviewer') {
      document.getElementById('navReview')?.classList.add('active');
      this.startReviewSession();
    }
    if (view === 'graph') {
      document.getElementById('navGraph')?.classList.add('active');
      this.initGraphView();
    }
    if (view === 'pdfs') {
      document.getElementById('navPDFs')?.classList.add('active');
      this.loadPdfLibrary();
    }
  }

  // Data Loading
  async loadDocuments() {
    try {
      const res = await this.fetchAPI('/api/tree');
      if (!res || !res.ok) return;
      this.documents = await res.json();
      this.renderDocsList();
    } catch (e) {
      console.error('Failed to load documents', e);
    }
  }

  async refreshDueBadge() {
    try {
      const res = await this.fetchAPI('/api/cards/stats');
      if (!res || !res.ok) return;
      const stats = await res.json();
      const badge = document.getElementById('dueBadge');
      if (badge) {
        badge.innerText = stats.due_today;
        badge.style.display = stats.due_today > 0 ? 'inline-block' : 'none';
      }
    } catch (e) {
      console.error('Failed to refresh stats', e);
    }
  }

  renderDocsList() {
    const list = document.getElementById('docsList');
    list.innerHTML = '';

    this.documents.forEach(doc => {
      const item = document.createElement('div');
      item.className = 'doc-item' + (doc.id === this.currentDocID ? ' active' : '');
      item.onclick = () => this.zoomTo(doc.id);

      const title = this.cleanText(doc.content) || 'Untitled Document';
      item.innerHTML = `<span>📄</span><span style="overflow:hidden;text-overflow:ellipsis;">${this.escapeHTML(title)}</span>`;
      list.appendChild(item);
    });
  }

  async createNewDoc(defaultTitle = 'Untitled Document') {
    try {
      const res = await this.fetchAPI('/api/rems', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: defaultTitle, parent_id: null })
      });
      if (!res || !res.ok) return;
      const doc = await res.json();
      // Add a starter child bullet
      await this.fetchAPI('/api/rems', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: 'Welcome :: Type here to take notes', parent_id: doc.id })
      });

      await this.loadDocuments();
      await this.zoomTo(doc.id);
    } catch (e) {
      console.error('Failed to create new document', e);
    }
  }

  async zoomTo(remID) {
    this.currentDocID = remID;
    this.renderDocsList();
    this.switchView('outliner');

    if (!remID) {
      // Home view
      this.breadcrumbs = [];
      this.renderBreadcrumbs();
      const res = await this.fetchAPI('/api/tree');
      if (!res || !res.ok) return;
      this.currentTree = await res.json();
      document.getElementById('docTitleInput').value = 'All Documents';
      document.getElementById('docTitleInput').disabled = true;
      this.renderOutlinerTree(this.currentTree);
      document.getElementById('backlinksSection').style.display = 'none';
      return;
    }

    try {
      // Fetch rem details + ancestors
      const remRes = await this.fetchAPI(`/api/rems/${remID}`);
      if (!remRes || !remRes.ok) return;
      const data = await remRes.json();
      this.currentRem = data.rem;
      this.breadcrumbs = data.ancestors || [];

      // Fetch subtree
      const treeRes = await this.fetchAPI(`/api/tree?root_id=${remID}`);
      if (!treeRes || !treeRes.ok) return;
      const tree = await treeRes.json();
      this.currentTree = (tree && tree.length > 0 && tree[0].children) ? tree[0].children : [];

      // Update Header
      const titleInput = document.getElementById('docTitleInput');
      titleInput.disabled = false;
      titleInput.value = this.cleanText(this.currentRem.content);

      document.getElementById('docCardsMeta').innerText = `${tree[0]?.card_count || 0} cards`;
      this.renderBreadcrumbs();
      this.renderOutlinerTree(this.currentTree);
      this.renderBacklinks(data.backlinks || []);
    } catch (e) {
      console.error('Failed to zoom to rem', e);
    }
  }

  loadDocument(id) {
    return this.zoomTo(id);
  }

  renderBreadcrumbs() {
    const bar = document.getElementById('breadcrumbsBar');
    bar.innerHTML = '';

    const home = document.createElement('span');
    home.className = 'breadcrumb-item';
    home.innerText = '🏠 Home';
    home.onclick = () => this.zoomTo(null);
    bar.appendChild(home);

    this.breadcrumbs.forEach((ancestor, index) => {
      const sep = document.createElement('span');
      sep.className = 'breadcrumb-sep';
      sep.innerText = '/';
      bar.appendChild(sep);

      const item = document.createElement('span');
      item.className = 'breadcrumb-item' + (index === this.breadcrumbs.length - 1 ? ' active' : '');
      item.innerText = this.cleanText(ancestor.content) || 'Untitled';
      item.onclick = () => this.zoomTo(ancestor.id);
      bar.appendChild(item);
    });
  }

  renderOutlinerTree(tree) {
    const root = document.getElementById('bulletTreeRoot');
    root.innerHTML = '';

    if (!tree || tree.length === 0) {
      const emptyRow = document.createElement('li');
      emptyRow.className = 'bullet-item';
      emptyRow.innerHTML = `
        <div class="bullet-row">
          <div class="bullet-dot-wrapper"><div class="bullet-dot"></div></div>
          <div class="bullet-input-wrapper">
            <div class="bullet-editor" contenteditable="true" data-id="new" placeholder="Type here or press Tab..."></div>
          </div>
        </div>
      `;
      root.appendChild(emptyRow);
      this.bindEditorEvents(emptyRow.querySelector('.bullet-editor'), null);
      return;
    }

    tree.forEach(node => {
      root.appendChild(this.createBulletNode(node));
    });
  }

  createBulletNode(node) {
    const li = document.createElement('li');
    li.className = 'bullet-item';
    li.dataset.id = node.id;

    const row = document.createElement('div');
    row.className = 'bullet-row';

    // Chevron Toggle
    const toggle = document.createElement('div');
    const hasKids = node.children && node.children.length > 0;
    toggle.className = 'bullet-toggle' + (hasKids ? '' : ' hidden') + (node.collapsed ? ' collapsed' : '');
    toggle.innerHTML = `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"></polyline></svg>`;
    toggle.onclick = (e) => {
      e.stopPropagation();
      this.toggleCollapse(node.id, li);
    };

    // Bullet Dot
    const dotWrapper = document.createElement('div');
    dotWrapper.className = 'bullet-dot-wrapper';
    dotWrapper.title = 'Click to zoom';
    dotWrapper.onclick = () => this.zoomTo(node.id);
    const dot = document.createElement('div');
    dot.className = 'bullet-dot' + (hasKids ? ' has-children' : '');
    dotWrapper.appendChild(dot);

    // Editor & Badges
    const inputWrapper = document.createElement('div');
    inputWrapper.className = 'bullet-input-wrapper';

    const editor = document.createElement('div');
    editor.className = 'bullet-editor';
    editor.contentEditable = 'true';
    editor.innerText = node.content;
    editor.dataset.id = node.id;
    this.bindEditorEvents(editor, node);

    inputWrapper.appendChild(editor);

    // Badges for cards / delimiters
    const badge = this.createCardBadge(node.content);
    if (badge) inputWrapper.appendChild(badge);

    // PDF Pin Badges
    const pinBadges = this.createPdfPinBadges(node.content);
    pinBadges.forEach(b => inputWrapper.appendChild(b));

    row.appendChild(toggle);
    row.appendChild(dotWrapper);
    row.appendChild(inputWrapper);
    li.appendChild(row);

    // Child List
    const childUl = document.createElement('ul');
    childUl.className = 'bullet-children' + (node.collapsed ? ' collapsed' : '');
    if (node.children && node.children.length > 0) {
      node.children.forEach(child => {
        childUl.appendChild(this.createBulletNode(child));
      });
    }
    li.appendChild(childUl);

    return li;
  }

  createCardBadge(content) {
    if (!content) return null;
    const span = document.createElement('span');

    if (content.includes(':::')) {
      span.className = 'bullet-badge';
      span.innerText = '⇄ 2-way card';
      return span;
    }
    if (content.includes('::')) {
      span.className = 'bullet-badge';
      span.innerText = '→ card';
      return span;
    }
    if (content.includes(';;')) {
      span.className = 'bullet-badge desc';
      span.innerText = '% descriptor';
      return span;
    }
    if (content.includes('==>')) {
      span.className = 'bullet-badge';
      span.innerText = '⇒ list card';
      return span;
    }
    if (content.includes('{{') && content.includes('}}')) {
      span.className = 'bullet-badge cloze';
      span.innerText = '[...] cloze';
      return span;
    }
    return null;
  }

  bindEditorEvents(editor, node) {
    editor.addEventListener('focus', () => {
      this.lastFocusedRemID = editor.dataset.id;
      this.lastFocusedParentID = node?.parent_id || this.currentDocID;
    });

    let timeout = null;
    editor.addEventListener('input', () => {
      clearTimeout(timeout);
      timeout = setTimeout(async () => {
        const text = editor.innerText.trim();
        const remID = editor.dataset.id;

        const existingBadges = editor.parentElement.querySelectorAll('.bullet-badge');
        existingBadges.forEach(b => b.remove());
        const newBadge = this.createCardBadge(text);
        if (newBadge) editor.parentElement.appendChild(newBadge);
        const pinBadges = this.createPdfPinBadges(text);
        pinBadges.forEach(b => editor.parentElement.appendChild(b));

        if (remID && remID !== 'new') {
          await this.fetchAPI(`/api/rems/${remID}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ content: text })
          });
          this.refreshDueBadge();
        } else if (text !== '') {
          const parentID = this.currentDocID;
          const res = await this.fetchAPI('/api/rems', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ content: text, parent_id: parentID })
          });
          if (res && res.ok) {
            const newRem = await res.json();
            editor.dataset.id = newRem.id;
            node = newRem;
            this.refreshDueBadge();
          }
        }
      }, 300);
    });

    editor.addEventListener('keydown', async (e) => {
      const id = editor.dataset.id;
      if (!id || id === 'new') return;

      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        const parentID = node?.parent_id || this.currentDocID;
        const res = await this.fetchAPI('/api/rems', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content: '', parent_id: parentID, after_id: id })
        });
        if (res && res.ok) {
          const created = await res.json();
          await this.reloadCurrentView();
          this.focusEditorByID(created.id);
        }
      } else if (e.key === 'Tab') {
        e.preventDefault();
        if (e.shiftKey) {
          // Outdent
          const res = await this.fetchAPI(`/api/rems/${id}/outdent`, { method: 'POST' });
          if (res && res.ok) {
            await this.reloadCurrentView();
            this.focusEditorByID(id);
          }
        } else {
          // Indent
          const res = await this.fetchAPI(`/api/rems/${id}/indent`, { method: 'POST' });
          if (res && res.ok) {
            await this.reloadCurrentView();
            this.focusEditorByID(id);
          }
        }
      } else if (e.key === 'Backspace' && editor.innerText.trim() === '') {
        e.preventDefault();
        const allEditors = Array.from(document.querySelectorAll('.bullet-editor'));
        const currentIdx = allEditors.indexOf(editor);
        let prevID = null;
        if (allEditors.length > 1 && currentIdx > 0) {
          prevID = allEditors[currentIdx - 1].dataset.id;
        }
        await this.fetchAPI(`/api/rems/${id}`, { method: 'DELETE' });
        await this.reloadCurrentView();
        if (prevID) {
          this.focusEditorByID(prevID);
        }
      } else if (e.key === 'ArrowUp') {
        const allEditors = Array.from(document.querySelectorAll('.bullet-editor'));
        const idx = allEditors.indexOf(editor);
        if (idx > 0) {
          e.preventDefault();
          allEditors[idx - 1].focus();
        }
      } else if (e.key === 'ArrowDown') {
        const allEditors = Array.from(document.querySelectorAll('.bullet-editor'));
        const idx = allEditors.indexOf(editor);
        if (idx !== -1 && idx + 1 < allEditors.length) {
          e.preventDefault();
          allEditors[idx + 1].focus();
        }
      }
    });
  }

  focusEditorByID(id) {
    setTimeout(() => {
      const el = document.querySelector(`.bullet-editor[data-id="${id}"]`);
      if (el) {
        el.focus();
        const range = document.createRange();
        range.selectNodeContents(el);
        range.collapse(false);
        const sel = window.getSelection();
        sel.removeAllRanges();
        sel.addRange(range);
      }
    }, 50);
  }

  async toggleCollapse(id, liElement) {
    try {
      const res = await this.fetchAPI(`/api/rems/${id}/toggle-collapse`, { method: 'POST' });
      if (!res || !res.ok) return;
      const data = await res.json();
      const toggle = liElement.querySelector('.bullet-toggle');
      const childUl = liElement.querySelector('.bullet-children');

      if (data.collapsed) {
        toggle.classList.add('collapsed');
        childUl.classList.add('collapsed');
      } else {
        toggle.classList.remove('collapsed');
        childUl.classList.remove('collapsed');
      }
    } catch (e) {
      console.error('Failed to toggle collapse', e);
    }
  }

  onTitleChange(newTitle) {
    if (!this.currentDocID) return;
    clearTimeout(this.titleTimeout);
    this.titleTimeout = setTimeout(async () => {
      try {
        await this.fetchAPI(`/api/rems/${this.currentDocID}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content: newTitle })
        });
        await this.loadDocuments();
      } catch (e) {
        console.error('Failed to update title', e);
      }
    }, 300);
  }

  renderBacklinks(backlinks) {
    const section = document.getElementById('backlinksSection');
    const list = document.getElementById('backlinksList');
    const countEl = document.getElementById('backlinksCount');

    if (!backlinks || backlinks.length === 0) {
      section.style.display = 'none';
      return;
    }

    section.style.display = 'block';
    countEl.innerText = backlinks.length;
    list.innerHTML = '';

    backlinks.forEach(rem => {
      const card = document.createElement('div');
      card.className = 'backlink-card';
      card.onclick = () => this.zoomTo(rem.id);
      card.innerHTML = `<div style="font-size:0.9rem;">${this.escapeHTML(rem.content)}</div>`;
      list.appendChild(card);
    });
  }

  async reloadCurrentView() {
    if (this.currentDocID) {
      await this.zoomTo(this.currentDocID);
    } else {
      await this.zoomTo(null);
    }
  }

  // Flashcard Reviewer Session (FSRS)
  async startReviewSession() {
    this.currentCardIndex = 0;
    this.isAnswerRevealed = false;

    const endpoint = this.isCramMode ? '/api/cards/cram' : '/api/cards/due';
    try {
      const res = await this.fetchAPI(endpoint);
      if (!res || !res.ok) return;
      this.dueCards = await res.json();
      this.updateReviewerCounters();
      this.renderCurrentCard();
    } catch (e) {
      console.error('Failed to fetch due cards', e);
    }
  }

  toggleCramMode() {
    this.isCramMode = !this.isCramMode;
    const btn = document.getElementById('cramToggleBtn');
    btn.innerHTML = this.isCramMode ? '<span>⚡ Cram Active</span>' : '<span>⚡ Cram All</span>';
    btn.classList.toggle('btn-primary', this.isCramMode);
    this.startReviewSession();
  }

  updateReviewerCounters() {
    let n = 0, l = 0, r = 0;
    this.dueCards.slice(this.currentCardIndex).forEach(c => {
      if (c.state === 0) n++;
      else if (c.state === 1 || c.state === 3) l++;
      else r++;
    });
    document.getElementById('pillNew').innerText = `${n} New`;
    document.getElementById('pillLearn').innerText = `${l} Learn`;
    document.getElementById('pillReview').innerText = `${r} Review`;
  }

  renderCurrentCard() {
    const promptEl = document.getElementById('cardPrompt');
    const answerWrapper = document.getElementById('cardAnswerWrapper');
    const answerEl = document.getElementById('cardAnswer');
    const crumbsEl = document.getElementById('cardBreadcrumbs');
    const showBtn = document.getElementById('showAnswerBtn');
    const ratingGrid = document.getElementById('ratingGrid');

    if (this.currentCardIndex >= this.dueCards.length) {
      promptEl.innerHTML = `🎉 All done! You've reviewed all due flashcards.`;
      crumbsEl.innerHTML = '';
      answerWrapper.style.display = 'none';
      showBtn.style.display = 'none';
      ratingGrid.style.display = 'none';
      this.refreshDueBadge();
      return;
    }

    const card = this.dueCards[this.currentCardIndex];
    this.isAnswerRevealed = false;

    crumbsEl.innerHTML = (card.breadcrumbs || []).map(b => this.escapeHTML(b)).join(' > ');
    promptEl.innerText = card.front;
    answerEl.innerText = card.back;

    // Attach interactive PDF Pin badges if flashcard references PDF highlights
    const pinBadges = this.createPdfPinBadges(card.back + ' ' + (card.front || ''));
    if (pinBadges && pinBadges.length > 0) {
      const pinContainer = document.createElement('div');
      pinContainer.style.marginTop = '12px';
      pinContainer.style.display = 'flex';
      pinContainer.style.flexWrap = 'wrap';
      pinContainer.style.gap = '6px';
      pinBadges.forEach(b => pinContainer.appendChild(b));
      answerEl.appendChild(pinContainer);
    }

    answerWrapper.style.display = 'none';

    if (card.next_previews) {
      document.getElementById('rateAgainIntvl').innerText = card.next_previews.again.label;
      document.getElementById('rateHardIntvl').innerText = card.next_previews.hard.label;
      document.getElementById('rateGoodIntvl').innerText = card.next_previews.good.label;
      document.getElementById('rateEasyIntvl').innerText = card.next_previews.easy.label;
    }

    showBtn.style.display = 'block';
    ratingGrid.style.display = 'none';
    this.updateReviewerCounters();
  }

  revealAnswer() {
    if (this.currentCardIndex >= this.dueCards.length) return;
    this.isAnswerRevealed = true;
    document.getElementById('cardAnswerWrapper').style.display = 'block';
    document.getElementById('showAnswerBtn').style.display = 'none';
    document.getElementById('ratingGrid').style.display = 'grid';
  }

  async rateCard(rating) {
    if (this.currentCardIndex >= this.dueCards.length) return;
    const card = this.dueCards[this.currentCardIndex];

    try {
      await this.fetchAPI(`/api/cards/${card.id}/review`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ rating: rating, is_cram: this.isCramMode })
      });
    } catch (e) {
      console.error('Failed to submit review', e);
    }

    this.currentCardIndex++;
    this.renderCurrentCard();
    this.refreshDueBadge();
  }

  // Interactive 2D Knowledge Graph (Canvas)
  async initGraphView() {
    const canvas = document.getElementById('graphCanvas');
    const ctx = canvas.getContext('2d');
    const rect = canvas.getBoundingClientRect();
    canvas.width = rect.width;
    canvas.height = rect.height;

    try {
      const res = await this.fetchAPI('/api/graph');
      if (!res || !res.ok) return;
      this.graphData = await res.json();
    } catch (e) {
      console.error('Failed to load graph', e);
      return;
    }

    const width = canvas.width;
    const height = canvas.height;

    const numNodes = this.graphData.nodes.length;
    this.graphNodes = this.graphData.nodes.map((n, i) => {
      const angle = (i / (numNodes || 1)) * Math.PI * 2;
      const radius = Math.min(width, height) * 0.35;
      return {
        ...n,
        x: width / 2 + Math.cos(angle) * radius + (Math.random() - 0.5) * 40,
        y: height / 2 + Math.sin(angle) * radius + (Math.random() - 0.5) * 40,
        vx: 0,
        vy: 0,
        radius: n.is_root ? 12 : 7
      };
    });

    const nodeIndex = new Map();
    this.graphNodes.forEach((n, i) => nodeIndex.set(n.id, i));

    this.graphEdges = (this.graphData.edges || []).map(e => ({
      source: nodeIndex.get(e.source),
      target: nodeIndex.get(e.target),
      type: e.type
    })).filter(e => e.source !== undefined && e.target !== undefined);

    canvas.onmousedown = (e) => {
      const mx = e.offsetX;
      const my = e.offsetY;
      for (const node of this.graphNodes) {
        const dist = Math.hypot(node.x - mx, node.y - my);
        if (dist <= node.radius + 6) {
          this.dragNode = node;
          break;
        }
      }
    };

    canvas.onmousemove = (e) => {
      if (this.dragNode) {
        this.dragNode.x = e.offsetX;
        this.dragNode.y = e.offsetY;
        this.dragNode.vx = 0;
        this.dragNode.vy = 0;
      }
    };

    canvas.onmouseup = (e) => {
      if (this.dragNode) {
        const dist = Math.hypot(this.dragNode.vx, this.dragNode.vy);
        if (dist < 1) {
          this.zoomTo(this.dragNode.id);
        }
      }
      this.dragNode = null;
    };

    this.startGraphPhysics(canvas, ctx);
  }

  startGraphPhysics(canvas, ctx) {
    if (this.graphAnimId) cancelAnimationFrame(this.graphAnimId);

    const step = () => {
      if (this.activeView !== 'graph') return;
      const width = canvas.width;
      const height = canvas.height;

      // 1. Repulsion between all nodes
      for (let i = 0; i < this.graphNodes.length; i++) {
        for (let j = i + 1; j < this.graphNodes.length; j++) {
          const n1 = this.graphNodes[i];
          const n2 = this.graphNodes[j];
          let dx = n2.x - n1.x;
          let dy = n2.y - n1.y;
          let dist = Math.hypot(dx, dy) || 1;
          if (dist < 260) {
            const force = (260 - dist) / dist * 0.08;
            if (n1 !== this.dragNode) { n1.vx -= dx * force; n1.vy -= dy * force; }
            if (n2 !== this.dragNode) { n2.vx += dx * force; n2.vy += dy * force; }
          }
        }
      }

      // 2. Attraction along edges
      for (const edge of this.graphEdges) {
        const n1 = this.graphNodes[edge.source];
        const n2 = this.graphNodes[edge.target];
        let dx = n2.x - n1.x;
        let dy = n2.y - n1.y;
        let dist = Math.hypot(dx, dy) || 1;
        const targetDist = edge.type === 'reference' ? 120 : 70;
        const force = (dist - targetDist) * 0.005;
        if (n1 !== this.dragNode) { n1.vx += dx * force; n1.vy += dy * force; }
        if (n2 !== this.dragNode) { n2.vx += dx * force; n2.vy += dy * force; }
      }

      // 3. Center gravity & velocity dampening
      for (const n of this.graphNodes) {
        if (n === this.dragNode) continue;
        n.vx += (width / 2 - n.x) * 0.0008;
        n.vy += (height / 2 - n.y) * 0.0008;
        n.vx *= 0.88;
        n.vy *= 0.88;
        n.x += n.vx;
        n.y += n.vy;
      }

      // Draw
      ctx.clearRect(0, 0, width, height);

      // Edges
      ctx.lineWidth = 1.2;
      for (const edge of this.graphEdges) {
        const n1 = this.graphNodes[edge.source];
        const n2 = this.graphNodes[edge.target];
        ctx.strokeStyle = edge.type === 'reference' ? '#6366f199' : '#33415588';
        ctx.beginPath();
        ctx.moveTo(n1.x, n1.y);
        ctx.lineTo(n2.x, n2.y);
        ctx.stroke();
      }

      // Nodes
      for (const n of this.graphNodes) {
        ctx.beginPath();
        ctx.arc(n.x, n.y, n.radius, 0, Math.PI * 2);
        ctx.fillStyle = n.is_root ? '#6366f1' : '#a5b4fc';
        ctx.fill();
        ctx.lineWidth = 2;
        ctx.strokeStyle = '#1e293b';
        ctx.stroke();

        ctx.font = '11px sans-serif';
        ctx.fillStyle = '#94a3b8';
        ctx.textAlign = 'center';
        ctx.fillText(n.label, n.x, n.y + n.radius + 14);
      }

      this.graphAnimId = requestAnimationFrame(step);
    };

    this.graphAnimId = requestAnimationFrame(step);
  }

  resetGraphPhysics() {
    this.initGraphView();
  }

  // Global Keyboard Shortcuts
  bindGlobalShortcuts() {
    window.addEventListener('keydown', (e) => {
      // Search Modal: Cmd+K / Ctrl+K
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        this.openSearchModal();
        return;
      }

      // View switching: Cmd+1, Cmd+2, Cmd+3
      if ((e.metaKey || e.ctrlKey) && e.key === '1') {
        e.preventDefault();
        this.switchView('outliner');
        return;
      }
      if ((e.metaKey || e.ctrlKey) && e.key === '2') {
        e.preventDefault();
        this.switchView('reviewer');
        return;
      }
      if ((e.metaKey || e.ctrlKey) && e.key === '3') {
        e.preventDefault();
        this.switchView('graph');
        return;
      }
      if ((e.metaKey || e.ctrlKey) && e.key === '4') {
        e.preventDefault();
        this.switchView('pdfs');
        return;
      }

      // Reviewer shortcuts: Space (reveal answer), 1, 2, 3, 4 (rating)
      if (this.activeView === 'reviewer' && !this.isModalOpen()) {
        if (e.code === 'Space' && !this.isAnswerRevealed) {
          e.preventDefault();
          this.revealAnswer();
          return;
        }
        if (this.isAnswerRevealed && ['1', '2', '3', '4'].includes(e.key)) {
          e.preventDefault();
          this.rateCard(parseInt(e.key, 10));
          return;
        }
      }

      // Modal ESC to close
      if (e.key === 'Escape') {
        this.hidePdfSelectionTooltip();
        this.closeAllModals();
      }
    });
  }

  // Search Modal
  openSearchModal() {
    const modal = document.getElementById('searchModal');
    modal.classList.remove('hidden');
    const input = document.getElementById('searchInput');
    input.value = '';
    this.searchSelectedIndex = 0;
    input.focus();

    input.onkeydown = (e) => {
      const items = Array.from(document.querySelectorAll('.search-item'));
      if (items.length === 0) return;

      if (e.key === 'ArrowDown') {
        e.preventDefault();
        this.searchSelectedIndex = (this.searchSelectedIndex + 1) % items.length;
        this.updateSearchSelection();
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        this.searchSelectedIndex = (this.searchSelectedIndex - 1 + items.length) % items.length;
        this.updateSearchSelection();
      } else if (e.key === 'Enter') {
        e.preventDefault();
        if (items[this.searchSelectedIndex]) {
          items[this.searchSelectedIndex].click();
        }
      }
    };

    this.onSearchInput('');
  }

  updateSearchSelection() {
    const items = Array.from(document.querySelectorAll('.search-item'));
    items.forEach((it, idx) => {
      if (idx === this.searchSelectedIndex) {
        it.classList.add('selected');
        it.scrollIntoView({ block: 'nearest' });
      } else {
        it.classList.remove('selected');
      }
    });
  }

  async onSearchInput(val) {
    const trimmed = val.trim();
    const list = document.getElementById('searchResultsList');
    list.innerHTML = '';
    this.searchSelectedIndex = 0;

    if (!trimmed) {
      list.innerHTML = `<div style="padding:12px 18px;font-size:0.8rem;color:var(--text-muted);">Recent Documents</div>`;
      this.documents.slice(0, 5).forEach(doc => {
        const item = document.createElement('div');
        item.className = 'search-item';
        item.onclick = () => {
          this.zoomTo(doc.id);
          this.closeModal('searchModal');
        };
        item.innerHTML = `<span class="search-item-title">${this.escapeHTML(doc.content)}</span>`;
        list.appendChild(item);
      });
      this.updateSearchSelection();
      return;
    }

    try {
      const res = await this.fetchAPI(`/api/search?q=${encodeURIComponent(trimmed)}`);
      if (!res || !res.ok) return;
      const results = await res.json();
      if (!results || results.length === 0) {
        list.innerHTML = `<div style="padding:20px;text-align:center;color:var(--text-muted);">No matches found</div>`;
        return;
      }

      results.forEach(r => {
        const item = document.createElement('div');
        item.className = 'search-item';
        item.onclick = () => {
          this.zoomTo(r.id);
          this.closeModal('searchModal');
        };
        const breadcrumbStr = (r.breadcrumbs || []).join(' > ');
        item.innerHTML = `
          <span class="search-item-title">${r.snippet || this.escapeHTML(r.content)}</span>
          ${breadcrumbStr ? `<span class="search-item-crumbs">${this.escapeHTML(breadcrumbStr)}</span>` : ''}
        `;
        list.appendChild(item);
      });
      this.updateSearchSelection();
    } catch (e) {
      console.error('Search failed', e);
    }
  }

  // Export & Import Modal
  openImportExportModal() {
    // Add token query parameter to export links if authenticated
    const expMd = document.getElementById('exportMdLink');
    const expJson = document.getElementById('exportJsonLink');
    if (this.token) {
      expMd.href = `/api/export?token=${encodeURIComponent(this.token)}`;
      expJson.href = `/api/export?format=json&token=${encodeURIComponent(this.token)}`;
    } else {
      expMd.href = '/api/export';
      expJson.href = '/api/export?format=json';
    }
    document.getElementById('importExportModal').classList.remove('hidden');
  }

  async submitImport() {
    const textarea = document.getElementById('importTextarea');
    const text = textarea.value.trim();
    if (!text) return;

    try {
      const res = await this.fetchAPI('/api/import', {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: text
      });
      if (res && res.ok) {
        const data = await res.json();
        alert(data.message || 'Import successful!');
        this.closeModal('importExportModal');
        await this.loadDocuments();
        await this.zoomTo(null);
      }
    } catch (e) {
      alert('Import failed: ' + e.message);
    }
  }

  // Modal helpers
  isModalOpen() {
    return !document.getElementById('searchModal').classList.contains('hidden') ||
           !document.getElementById('importExportModal').classList.contains('hidden') ||
           !document.getElementById('authModal').classList.contains('hidden') ||
           !document.getElementById('apiKeysModal').classList.contains('hidden');
  }

  closeModal(modalID) {
    const m = document.getElementById(modalID);
    if (m) m.classList.add('hidden');
  }

  closeAllModals() {
    // If auth is required and user is not logged in, do not allow closing authModal
    if (this.authEnabled && !this.currentUser) {
      document.getElementById('searchModal').classList.add('hidden');
      document.getElementById('importExportModal').classList.add('hidden');
      document.getElementById('apiKeysModal').classList.add('hidden');
      return;
    }
    document.querySelectorAll('.modal-backdrop').forEach(m => m.classList.add('hidden'));
  }

  handleModalBackdropClick(event, modalID) {
    if (event.target.id === modalID) {
      // Prevent dismissing auth modal if user is not authenticated in auth-enabled mode
      if (modalID === 'authModal' && this.authEnabled && !this.currentUser) {
        return;
      }
      this.closeModal(modalID);
    }
  }

  cleanText(text) {
    if (!text) return '';
    return text.replace(/\[\[(.*?)\]\]/g, '$1')
               .replace(/\{\{c\d+::(.*?)\}\}/g, '$1')
               .replace(/\{\{(.*?)\}\}/g, '$1')
               .trim();
  }

  escapeHTML(str) {
    if (!str) return '';
    return str.replace(/&/g, '&amp;')
              .replace(/</g, '&lt;')
              .replace(/>/g, '&gt;')
              .replace(/"/g, '&quot;');
  }

  // ========================================================
  // PDF Library & Document Management
  // ========================================================

  async loadPdfLibrary() {
    try {
      const res = await this.fetchAPI('/api/pdfs');
      if (!res || !res.ok) return;
      this.pdfDocuments = await res.json();
      this.renderPdfDocumentsGrid();
      this.updatePdfCountBadge();
    } catch (e) {
      console.error('Failed to load PDF library', e);
    }
  }

  updatePdfCountBadge() {
    const badge = document.getElementById('pdfCountBadge');
    if (badge) {
      const count = this.pdfDocuments ? this.pdfDocuments.length : 0;
      badge.innerText = count;
      badge.style.display = count > 0 ? 'inline-block' : 'none';
    }
  }

  renderPdfDocumentsGrid() {
    const grid = document.getElementById('pdfDocumentsGrid');
    if (!grid) return;
    grid.innerHTML = '';

    if (!this.pdfDocuments || this.pdfDocuments.length === 0) {
      grid.innerHTML = `
        <div style="grid-column: 1 / -1; text-align: center; padding: 40px 20px; color: var(--text-muted);">
          <p style="font-size: 1.1rem; margin-bottom: 8px;">No PDF documents uploaded yet</p>
          <p style="font-size: 0.85rem;">Upload a PDF to start reading, highlighting quotes, and creating pinned excerpt backlinks.</p>
        </div>
      `;
      return;
    }

    this.pdfDocuments.forEach(doc => {
      const card = document.createElement('div');
      card.className = 'pdf-card';
      const sizeStr = (doc.file_size / (1024 * 1024)).toFixed(1) + ' MB';
      const dateStr = new Date(doc.created_at).toLocaleDateString();

      card.innerHTML = `
        <div class="pdf-card-main">
          <div class="pdf-card-icon">📄</div>
          <div class="pdf-card-info">
            <div class="pdf-card-title" title="${this.escapeHTML(doc.original_name)}">${this.escapeHTML(doc.original_name)}</div>
            <div class="pdf-card-meta">
              <span>${doc.page_count} pages</span>
              <span>•</span>
              <span>${sizeStr}</span>
              <span>•</span>
              <span>${dateStr}</span>
            </div>
          </div>
        </div>
        <div class="pdf-card-actions">
          <div class="pdf-card-btns-left">
            <button class="btn btn-sm btn-primary" onclick="app.openPdfReader('${doc.id}', 'split')">
              <span>📖 Split</span>
            </button>
            <button class="btn btn-sm" onclick="app.openPdfReader('${doc.id}', 'fullscreen')">
              <span>⛶ Full</span>
            </button>
          </div>
          <button class="btn btn-sm btn-danger btn-icon" onclick="app.confirmDeletePdf('${doc.id}')" title="Delete PDF">
            <span>🗑️</span>
          </button>
        </div>
      `;
      grid.appendChild(card);
    });
  }

  async confirmDeletePdf(docId) {
    if (!confirm('Are you sure you want to delete this PDF and all associated highlights?')) return;
    try {
      const res = await this.fetchAPI(`/api/pdfs/${docId}`, { method: 'DELETE' });
      if (res && res.ok) {
        if (this.currentPdfDoc && this.currentPdfDoc.id === docId) {
          this.closePdfReader();
          this.currentPdfDoc = null;
          this.currentPdf = null;
        }
        await this.loadPdfLibrary();
        this.showToast('PDF document deleted');
      }
    } catch (e) {
      console.error('Failed to delete PDF', e);
    }
  }

  handlePdfDragOver(e) {
    e.preventDefault();
    e.stopPropagation();
    const dropzone = document.getElementById('pdfDropzone');
    if (dropzone) dropzone.classList.add('dragover');
  }

  handlePdfDragLeave(e) {
    e.preventDefault();
    e.stopPropagation();
    const dropzone = document.getElementById('pdfDropzone');
    if (dropzone) dropzone.classList.remove('dragover');
  }

  handlePdfDrop(e) {
    e.preventDefault();
    e.stopPropagation();
    const dropzone = document.getElementById('pdfDropzone');
    if (dropzone) dropzone.classList.remove('dragover');

    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      const file = e.dataTransfer.files[0];
      if (file.type === 'application/pdf' || file.name.toLowerCase().endsWith('.pdf')) {
        this.uploadPdfFile(file);
      } else {
        alert('Please drop a valid PDF file.');
      }
    }
  }

  handlePdfFileSelected(e) {
    if (e.target.files && e.target.files.length > 0) {
      this.uploadPdfFile(e.target.files[0]);
    }
  }

  async uploadPdfFile(file) {
    const formData = new FormData();
    formData.append('file', file);

    try {
      this.showToast('Uploading PDF...');
      const res = await this.fetchAPI('/api/pdfs', {
        method: 'POST',
        body: formData
      });

      if (!res || !res.ok) {
        let errMsg = 'Upload failed';
        try {
          const errData = await res.json();
          errMsg = errData.error || errMsg;
        } catch (e) {}
        alert('Failed to upload PDF: ' + errMsg);
        return;
      }

      const newDoc = await res.json();
      await this.loadPdfLibrary();
      this.showToast(`Uploaded ${file.name}`);
      this.openPdfReader(newDoc.id, 'split');
    } catch (e) {
      console.error('Failed to upload PDF', e);
      alert('Upload failed: ' + e.message);
    }
  }

  // ========================================================
  // PDF Reader & Split-View Operations
  // ========================================================

  async openPdfReader(docId, mode = 'split') {
    this.switchView('outliner');
    const wrapper = document.getElementById('outlinerSplitWrapper');
    const pane = document.getElementById('pdfReaderPane');
    const resizer = document.getElementById('splitResizer');

    this.isPdfSplitActive = true;
    if (wrapper) wrapper.classList.add('split-active');
    if (pane) pane.classList.remove('hidden');
    if (resizer) resizer.classList.remove('hidden');

    if (mode === 'fullscreen') {
      this.isPdfFullscreen = true;
      if (wrapper) wrapper.classList.add('pdf-fullscreen');
      const btn = document.getElementById('pdfFullscreenBtn');
      if (btn) {
        btn.innerText = '◫';
        btn.title = 'Switch to Split View';
      }
    } else {
      this.isPdfFullscreen = false;
      if (wrapper) wrapper.classList.remove('pdf-fullscreen');
      const btn = document.getElementById('pdfFullscreenBtn');
      if (btn) {
        btn.innerText = '⛶';
        btn.title = 'Maximize PDF';
      }
    }

    await this.loadPdfDocument(docId);
  }

  async loadPdfDocument(docId) {
    if (!window.pdfjsLib) {
      console.error('PDF.js library is not available');
      return;
    }

    window.pdfjsLib.GlobalWorkerOptions.workerSrc = '/pdfjs/pdf.worker.min.js';

    // 1. Fetch document metadata
    const docRes = await this.fetchAPI(`/api/pdfs/${docId}`);
    if (!docRes || !docRes.ok) return;
    this.currentPdfDoc = await docRes.json();

    // 2. Fetch highlights
    const hlRes = await this.fetchAPI(`/api/pdfs/${docId}/highlights`);
    this.pdfHighlights = (hlRes && hlRes.ok) ? await hlRes.json() : [];

    // Update Toolbar UI
    const titleEl = document.getElementById('pdfToolbarTitle');
    if (titleEl) titleEl.innerText = this.currentPdfDoc.original_name;
    const totalEl = document.getElementById('pdfTotalPages');
    if (totalEl) totalEl.innerText = this.currentPdfDoc.page_count;
    const pageInput = document.getElementById('pdfPageInput');
    if (pageInput) {
      pageInput.max = this.currentPdfDoc.page_count;
      pageInput.value = 1;
    }
    this.currentPdfPage = 1;
    this.updateHighlightsDrawer();

    // 3. Load PDF via PDF.js
    const contentUrl = `/api/pdfs/${docId}/content?token=${encodeURIComponent(this.token || '')}`;
    const loadingTask = window.pdfjsLib.getDocument({
      url: contentUrl,
      withCredentials: true
    });

    try {
      this.currentPdf = await loadingTask.promise;
    } catch (err) {
      console.error('Failed to load PDF document via PDF.js:', err);
      return;
    }

    // 4. Setup pages stage
    const stage = document.getElementById('pdfPagesStage');
    if (!stage) return;
    stage.innerHTML = '';
    this.renderedPdfPages.clear();
    this.renderingPdfPages.clear();

    const numPages = this.currentPdf.numPages;
    if (totalEl) totalEl.innerText = numPages;
    if (pageInput) pageInput.max = numPages;

    // Sync real decoded page count with backend if discrepancy exists
    if (numPages && this.currentPdfDoc.page_count !== numPages) {
      this.currentPdfDoc.page_count = numPages;
      this.fetchAPI(`/api/pdfs/${docId}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ page_count: numPages })
      }).catch(e => console.error('Failed to sync PDF page count', e));
    }

    for (let pageNum = 1; pageNum <= numPages; pageNum++) {
      const pageWrapper = document.createElement('div');
      pageWrapper.className = 'pdf-page-wrapper';
      pageWrapper.dataset.pageNumber = pageNum;
      pageWrapper.id = `pdf-page-${pageNum}`;
      pageWrapper.style.minHeight = '700px';
      pageWrapper.style.width = '550px';

      const pageInner = document.createElement('div');
      pageInner.className = 'pdf-page-inner';

      const canvas = document.createElement('canvas');
      canvas.className = 'pdf-canvas';

      const highlightLayer = document.createElement('div');
      highlightLayer.className = 'pdf-highlight-layer';

      const textLayer = document.createElement('div');
      textLayer.className = 'pdf-text-layer textLayer';

      pageInner.appendChild(canvas);
      pageInner.appendChild(highlightLayer);
      pageInner.appendChild(textLayer);
      pageWrapper.appendChild(pageInner);
      stage.appendChild(pageWrapper);
    }

    // Render first 2 pages immediately
    await this.renderPdfPage(1);
    if (numPages >= 2) {
      await this.renderPdfPage(2);
    }

    this.onPdfViewportScroll();
  }

  async renderPdfPage(pageNum) {
    if (!this.currentPdf || pageNum < 1 || pageNum > this.currentPdf.numPages) return;
    if (this.renderedPdfPages.has(pageNum) || this.renderingPdfPages.has(pageNum)) return;

    this.renderingPdfPages.add(pageNum);

    try {
      const page = await this.currentPdf.getPage(pageNum);
      const viewport = page.getViewport({ scale: this.pdfScale });

      const wrapper = document.getElementById(`pdf-page-${pageNum}`);
      if (!wrapper) return;

      wrapper.style.width = `${viewport.width}px`;
      wrapper.style.minHeight = `${viewport.height}px`;

      const pageInner = wrapper.querySelector('.pdf-page-inner');
      if (pageInner) {
        pageInner.style.width = `${viewport.width}px`;
        pageInner.style.height = `${viewport.height}px`;
      }

      // 1. Render Canvas
      const canvas = wrapper.querySelector('.pdf-canvas');
      const dpr = window.devicePixelRatio || 1;
      canvas.width = viewport.width * dpr;
      canvas.height = viewport.height * dpr;
      canvas.style.width = `${viewport.width}px`;
      canvas.style.height = `${viewport.height}px`;

      const ctx = canvas.getContext('2d');
      ctx.scale(dpr, dpr);

      await page.render({ canvasContext: ctx, viewport: viewport }).promise;

      // 2. Render Text Layer
      const textLayer = wrapper.querySelector('.pdf-text-layer');
      if (textLayer) {
        textLayer.innerHTML = '';
        textLayer.style.width = `${viewport.width}px`;
        textLayer.style.height = `${viewport.height}px`;
        textLayer.style.setProperty('--scale-factor', viewport.scale);

        const textContent = await page.getTextContent();
        if (window.pdfjsLib && window.pdfjsLib.renderTextLayer) {
          const textTask = window.pdfjsLib.renderTextLayer({
            textContentSource: textContent,
            container: textLayer,
            viewport: viewport,
            textDivs: []
          });
          if (textTask && textTask.promise) {
            await textTask.promise;
          }
        }
      }

      // 3. Render Highlight Layer
      this.renderHighlightsForPage(pageNum, wrapper.querySelector('.pdf-highlight-layer'));

      this.renderedPdfPages.add(pageNum);
    } catch (err) {
      console.error(`Error rendering page ${pageNum}:`, err);
    } finally {
      this.renderingPdfPages.delete(pageNum);
    }
  }

  renderHighlightsForPage(pageNum, layerEl) {
    if (!layerEl) return;
    layerEl.innerHTML = '';

    const pageHls = this.pdfHighlights.filter(h => h.page_number === pageNum);
    pageHls.forEach(hl => {
      let rects = [];
      try {
        rects = JSON.parse(hl.rects_json);
      } catch (e) {
        rects = [];
      }
      if (!Array.isArray(rects)) rects = [rects];

      rects.forEach((rect, idx) => {
        const box = document.createElement('div');
        box.className = 'pdf-highlight-box';
        box.id = `hl-${hl.id}${idx > 0 ? '-' + idx : ''}`;
        box.dataset.highlightId = hl.id;
        box.style.left = `${(rect.x || 0) * 100}%`;
        box.style.top = `${(rect.y || 0) * 100}%`;
        box.style.width = `${(rect.w || 0) * 100}%`;
        box.style.height = `${(rect.h || 0) * 100}%`;
        box.style.backgroundColor = hl.color || '#ffeb3b';
        box.title = hl.text_content;

        box.onclick = (e) => {
          e.stopPropagation();
          this.flashHighlight(hl.id);
        };
        layerEl.appendChild(box);
      });
    });
  }

  onPdfViewportScroll() {
    if (!this.currentPdf) return;
    const viewport = document.getElementById('pdfScrollViewport');
    if (!viewport) return;

    const scrollTop = viewport.scrollTop;
    const viewportHeight = viewport.clientHeight;
    const numPages = this.currentPdf.numPages;

    let activePage = 1;
    for (let i = 1; i <= numPages; i++) {
      const pageEl = document.getElementById(`pdf-page-${i}`);
      if (!pageEl) continue;

      const pageTop = pageEl.offsetTop;
      const pageHeight = pageEl.offsetHeight;

      if (pageTop <= scrollTop + viewportHeight / 3) {
        activePage = i;
      }

      // Render if in or near viewport (600px lookahead)
      if (pageTop + pageHeight >= scrollTop - 600 && pageTop <= scrollTop + viewportHeight + 600) {
        if (!this.renderedPdfPages.has(i) && !this.renderingPdfPages.has(i)) {
          this.renderPdfPage(i);
        }
      }
    }

    if (this.currentPdfPage !== activePage) {
      this.currentPdfPage = activePage;
      const pageInput = document.getElementById('pdfPageInput');
      if (pageInput) pageInput.value = activePage;
    }
  }

  async jumpToPdfPage(pageNum, skipScroll = false) {
    if (!this.currentPdf || pageNum < 1 || pageNum > this.currentPdf.numPages) return;
    this.currentPdfPage = pageNum;
    const pageInput = document.getElementById('pdfPageInput');
    if (pageInput) pageInput.value = pageNum;

    await this.renderPdfPage(pageNum);

    if (!skipScroll) {
      const pageEl = document.getElementById(`pdf-page-${pageNum}`);
      if (pageEl) {
        pageEl.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }
    }
  }

  prevPdfPage() {
    if (this.currentPdfPage > 1) {
      this.jumpToPdfPage(this.currentPdfPage - 1);
    }
  }

  nextPdfPage() {
    if (this.currentPdf && this.currentPdfPage < this.currentPdf.numPages) {
      this.jumpToPdfPage(this.currentPdfPage + 1);
    }
  }

  async zoomPdf(delta) {
    let newScale = Math.round((this.pdfScale + delta) * 100) / 100;
    if (newScale < 0.4) newScale = 0.4;
    if (newScale > 3.0) newScale = 3.0;
    this.pdfScale = newScale;
    const zoomText = document.getElementById('pdfZoomLevel');
    if (zoomText) zoomText.innerText = Math.round(this.pdfScale * 100) + '%';

    this.renderedPdfPages.clear();
    this.renderingPdfPages.clear();
    const stage = document.getElementById('pdfPagesStage');
    if (stage) {
      Array.from(stage.children).forEach(w => {
        w.style.width = '';
        w.style.minHeight = '';
      });
    }
    this.onPdfViewportScroll();
  }

  async fitPdfWidth() {
    if (!this.currentPdf) return;
    const viewport = document.getElementById('pdfScrollViewport');
    if (!viewport) return;
    const availableWidth = viewport.clientWidth - 40;
    try {
      const page = await this.currentPdf.getPage(this.currentPdfPage || 1);
      const standardViewport = page.getViewport({ scale: 1.0 });
      this.pdfScale = Math.max(0.4, Math.min(3.0, availableWidth / standardViewport.width));
      const zoomText = document.getElementById('pdfZoomLevel');
      if (zoomText) zoomText.innerText = Math.round(this.pdfScale * 100) + '%';
      this.renderedPdfPages.clear();
      this.renderingPdfPages.clear();
      this.onPdfViewportScroll();
    } catch (e) {
      console.error('Fit width error', e);
    }
  }

  togglePdfFullscreen() {
    const wrapper = document.getElementById('outlinerSplitWrapper');
    this.isPdfFullscreen = !this.isPdfFullscreen;
    const btn = document.getElementById('pdfFullscreenBtn');
    if (this.isPdfFullscreen) {
      if (wrapper) wrapper.classList.add('pdf-fullscreen');
      if (btn) {
        btn.innerText = '◫';
        btn.title = 'Switch to Split View';
      }
    } else {
      if (wrapper) wrapper.classList.remove('pdf-fullscreen');
      if (btn) {
        btn.innerText = '⛶';
        btn.title = 'Maximize PDF';
      }
    }
  }

  closePdfReader() {
    this.isPdfSplitActive = false;
    this.isPdfFullscreen = false;
    const wrapper = document.getElementById('outlinerSplitWrapper');
    const pane = document.getElementById('pdfReaderPane');
    const resizer = document.getElementById('splitResizer');

    if (wrapper) {
      wrapper.classList.remove('split-active');
      wrapper.classList.remove('pdf-fullscreen');
    }
    if (pane) pane.classList.add('hidden');
    if (resizer) resizer.classList.add('hidden');
    this.hidePdfSelectionTooltip();
  }

  togglePdfSplitView() {
    if (this.isPdfSplitActive) {
      this.closePdfReader();
    } else {
      if (this.currentPdfDoc) {
        this.openPdfReader(this.currentPdfDoc.id, 'split');
      } else if (this.pdfDocuments && this.pdfDocuments.length > 0) {
        this.openPdfReader(this.pdfDocuments[0].id, 'split');
      } else {
        this.switchView('pdfs');
      }
    }
  }

  togglePdfHighlightsDrawer() {
    const drawer = document.getElementById('pdfHighlightsDrawer');
    if (drawer) drawer.classList.toggle('hidden');
  }

  updateHighlightsDrawer() {
    const drawer = document.getElementById('pdfHighlightsList');
    const countSpan = document.getElementById('pdfHighlightsCount');
    if (!drawer) return;
    drawer.innerHTML = '';

    if (countSpan) countSpan.innerText = this.pdfHighlights.length;

    if (this.pdfHighlights.length === 0) {
      drawer.innerHTML = `
        <div style="padding: 16px 8px; text-align: center; color: var(--text-muted); font-size: 0.78rem;">
          No highlights yet.<br>Select text in the document to highlight and quote.
        </div>
      `;
      return;
    }

    this.pdfHighlights.forEach(hl => {
      const item = document.createElement('div');
      item.className = 'pdf-highlight-item';
      item.innerHTML = `
        <div class="pdf-hl-header">
          <span class="pdf-hl-page-tag">Page ${hl.page_number}</span>
          <button class="btn-icon-tiny" title="Delete highlight" style="font-size:0.75rem; color:var(--text-muted);">&times;</button>
        </div>
        <div class="pdf-hl-quote" style="border-left: 3px solid ${hl.color || '#ffeb3b'}; padding-left: 6px;">
          ${this.escapeHTML(hl.text_content)}
        </div>
      `;

      item.onclick = (e) => {
        if (e.target.tagName === 'BUTTON') return;
        this.jumpToPdfPage(hl.page_number);
        this.flashHighlight(hl.id);
      };

      const delBtn = item.querySelector('button');
      if (delBtn) {
        delBtn.onclick = async (e) => {
          e.stopPropagation();
          await this.deletePdfHighlight(hl.id);
        };
      }

      drawer.appendChild(item);
    });
  }

  async deletePdfHighlight(highlightID) {
    try {
      const res = await this.fetchAPI(`/api/highlights/${highlightID}`, { method: 'DELETE' });
      if (res && res.ok) {
        this.pdfHighlights = this.pdfHighlights.filter(h => h.id !== highlightID);
        const hlBoxes = document.querySelectorAll(`[data-highlight-id="${highlightID}"]`);
        hlBoxes.forEach(b => b.remove());
        this.updateHighlightsDrawer();
        this.showToast('Highlight removed');
      }
    } catch (e) {
      console.error('Failed to delete highlight', e);
    }
  }

  // ========================================================
  // PDF Pin Backlinks & Jumping
  // ========================================================

  parsePdfPins(content) {
    if (!content) return [];
    const pins = [];

    // Format 1: [[pdf:doc_id#...|Label]] or [[pdf:doc_id#...]]
    const regex1 = /\[\[pdf:([^|\]]+)(?:\|([^\]]+))?\]\]/g;
    let m1;
    while ((m1 = regex1.exec(content)) !== null) {
      const rawRef = m1[1];
      const label = m1[2];
      const [docId, hash] = rawRef.split('#');
      const params = new URLSearchParams(hash || '');
      const pageNum = parseInt(params.get('p') || params.get('page') || '1', 10);
      const highlightId = params.get('h') || params.get('highlight') || '';
      pins.push({
        docId: (docId || '').trim(),
        pageNum: pageNum > 0 ? pageNum : 1,
        highlightId: (highlightId || '').trim(),
        label: (label || `p.${pageNum}`).trim()
      });
    }

    // Format 2: [Label](pdf:doc_id#...)
    const regex2 = /\[([^\]]*)\]\(pdf:([^)]+)\)/g;
    let m2;
    while ((m2 = regex2.exec(content)) !== null) {
      const label = m2[1];
      const rawRef = m2[2];
      const [docId, hash] = rawRef.split('#');
      const params = new URLSearchParams(hash || '');
      const pageNum = parseInt(params.get('p') || params.get('page') || '1', 10);
      const highlightId = params.get('h') || params.get('highlight') || '';
      pins.push({
        docId: (docId || '').trim(),
        pageNum: pageNum > 0 ? pageNum : 1,
        highlightId: (highlightId || '').trim(),
        label: (label || `p.${pageNum}`).trim()
      });
    }

    return pins;
  }

  parsePdfPin(content) {
    const pins = this.parsePdfPins(content);
    return pins.length > 0 ? pins[0] : null;
  }

  createPdfPinBadges(content) {
    const pins = this.parsePdfPins(content);
    if (!pins || pins.length === 0) return [];

    return pins.map(pin => {
      const span = document.createElement('span');
      span.className = 'bullet-badge pdf-pin-badge';
      span.innerHTML = `📌 <span>${this.escapeHTML(pin.label || ('p.' + pin.pageNum))}</span>`;
      span.title = `Jump to PDF page ${pin.pageNum}`;
      span.onclick = (e) => {
        e.stopPropagation();
        this.jumpToPdfPin(pin.docId, pin.pageNum, pin.highlightId);
      };
      return span;
    });
  }

  createPdfPinBadge(content) {
    const badges = this.createPdfPinBadges(content);
    return badges.length > 0 ? badges[0] : null;
  }

  async jumpToPdfPin(docId, pageNum, highlightId) {
    this.switchView('outliner');

    // 1. Ensure Split View is active
    const wrapper = document.getElementById('outlinerSplitWrapper');
    const pane = document.getElementById('pdfReaderPane');
    const resizer = document.getElementById('splitResizer');

    this.isPdfSplitActive = true;
    if (wrapper) {
      wrapper.classList.add('split-active');
      wrapper.classList.remove('pdf-fullscreen');
    }
    this.isPdfFullscreen = false;
    if (pane) pane.classList.remove('hidden');
    if (resizer) resizer.classList.remove('hidden');

    // 2. Load PDF document if different
    if (!this.currentPdfDoc || this.currentPdfDoc.id !== docId) {
      await this.loadPdfDocument(docId);
    }

    // 3. Scroll to page (skip top scroll if highlight will center)
    await this.jumpToPdfPage(pageNum, Boolean(highlightId));

    // 4. Flash highlight
    if (highlightId) {
      this.flashHighlight(highlightId, pageNum);
    }
  }

  flashHighlight(highlightId, pageNum) {
    const tryFlash = () => {
      const boxes = document.querySelectorAll(`[data-highlight-id="${highlightId}"]`);
      if (boxes && boxes.length > 0) {
        boxes[0].scrollIntoView({ behavior: 'smooth', block: 'center' });
        boxes.forEach(b => {
          b.classList.remove('highlight-flash');
          void b.offsetWidth;
          b.classList.add('highlight-flash');
          setTimeout(() => b.classList.remove('highlight-flash'), 2500);
        });
        return true;
      }
      return false;
    };

    if (!tryFlash()) {
      setTimeout(() => {
        if (!tryFlash()) {
          setTimeout(() => {
            if (!tryFlash() && pageNum) {
              const pageEl = document.getElementById(`pdf-page-${pageNum}`);
              if (pageEl) pageEl.scrollIntoView({ behavior: 'smooth', block: 'start' });
            }
          }, 400);
        }
      }, 200);
    }
  }

  // ========================================================
  // PDF Text Selection & Excerpt Tooltip
  // ========================================================

  initPdfTextSelectionListener() {
    document.addEventListener('mouseup', (e) => {
      if (e.target.closest('#pdfSelectionTooltip')) return;

      const sel = window.getSelection();
      if (!sel || sel.isCollapsed || !sel.toString().trim()) {
        this.hidePdfSelectionTooltip();
        return;
      }

      const text = sel.toString().trim();
      const range = sel.getRangeAt(0);
      const pageWrapper = range.commonAncestorContainer.nodeType === Node.ELEMENT_NODE
        ? range.commonAncestorContainer.closest('.pdf-page-wrapper')
        : range.commonAncestorContainer.parentElement?.closest('.pdf-page-wrapper');

      if (!pageWrapper) {
        this.hidePdfSelectionTooltip();
        return;
      }

      const pageNum = parseInt(pageWrapper.dataset.pageNumber, 10);
      const pageInner = pageWrapper.querySelector('.pdf-page-inner');
      if (!pageInner) return;

      const innerRect = pageInner.getBoundingClientRect();
      const clientRects = range.getClientRects();
      if (clientRects.length === 0) {
        this.hidePdfSelectionTooltip();
        return;
      }

      const normalizedRects = [];
      for (let i = 0; i < clientRects.length; i++) {
        const r = clientRects[i];
        if (r.width > 0 && r.height > 0) {
          normalizedRects.push({
            x: Math.max(0, (r.left - innerRect.left) / innerRect.width),
            y: Math.max(0, (r.top - innerRect.top) / innerRect.height),
            w: Math.min(1, r.width / innerRect.width),
            h: Math.min(1, r.height / innerRect.height)
          });
        }
      }

      this.activePdfSelection = {
        text: text,
        pageNum: pageNum,
        rects: normalizedRects,
        docId: this.currentPdfDoc ? this.currentPdfDoc.id : null
      };

      const tooltip = document.getElementById('pdfSelectionTooltip');
      if (!tooltip) return;

      const lastRect = clientRects[clientRects.length - 1];
      const topPos = lastRect.bottom + 8;
      const leftPos = Math.max(20, Math.min(window.innerWidth - 360, lastRect.left + (lastRect.width / 2) - 130));

      tooltip.style.top = `${topPos}px`;
      tooltip.style.left = `${leftPos}px`;
      tooltip.classList.remove('hidden');
    });
  }

  setHighlightColor(color) {
    this.activeHighlightColor = color;
    document.querySelectorAll('.color-dot').forEach(d => {
      if (d.dataset.color === color) {
        d.classList.add('active');
      } else {
        d.classList.remove('active');
      }
    });
  }

  hidePdfSelectionTooltip() {
    const tooltip = document.getElementById('pdfSelectionTooltip');
    if (tooltip) tooltip.classList.add('hidden');
    this.activePdfSelection = null;
  }

  clearPdfSelection() {
    if (window.getSelection) {
      window.getSelection().removeAllRanges();
    }
    this.hidePdfSelectionTooltip();
  }

  async createHighlightFromSelection() {
    if (!this.activePdfSelection || !this.activePdfSelection.docId) return;
    const { text, pageNum, rects, docId } = this.activePdfSelection;

    try {
      const res = await this.fetchAPI(`/api/pdfs/${docId}/highlights`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          page_number: pageNum,
          rects_json: JSON.stringify(rects),
          text_content: text,
          color: this.activeHighlightColor || '#ffeb3b'
        })
      });

      if (res && res.ok) {
        const hl = await res.json();
        this.pdfHighlights.push(hl);

        const pageWrapper = document.getElementById(`pdf-page-${pageNum}`);
        if (pageWrapper) {
          this.renderHighlightsForPage(pageNum, pageWrapper.querySelector('.pdf-highlight-layer'));
        }
        this.updateHighlightsDrawer();
        this.clearPdfSelection();
        this.showToast('Highlight saved');
      }
    } catch (e) {
      console.error('Failed to create highlight', e);
    }
  }

  async extractExcerptToOutline() {
    if (!this.activePdfSelection || !this.activePdfSelection.docId) return;
    const { text, pageNum, rects, docId } = this.activePdfSelection;

    try {
      // 1. Save highlight
      const hlRes = await this.fetchAPI(`/api/pdfs/${docId}/highlights`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          page_number: pageNum,
          rects_json: JSON.stringify(rects),
          text_content: text,
          color: this.activeHighlightColor || '#ffeb3b'
        })
      });

      if (!hlRes || !hlRes.ok) return;
      const hl = await hlRes.json();
      this.pdfHighlights.push(hl);

      const pageWrapper = document.getElementById(`pdf-page-${pageNum}`);
      if (pageWrapper) {
        this.renderHighlightsForPage(pageNum, pageWrapper.querySelector('.pdf-highlight-layer'));
      }
      this.updateHighlightsDrawer();

      // 2. Prepare pin reference & bullet content
      const pinRef = `[[pdf:${docId}#p=${pageNum}&h=${hl.id}|p.${pageNum}]]`;
      const remContent = `"${text}" 📌 ${pinRef}`;

      // 3. Determine target document
      let targetDocID = this.currentDocID;
      let parentID = this.lastFocusedParentID || targetDocID;
      let afterID = this.lastFocusedRemID || null;

      if (!targetDocID) {
        const title = `Notes on ${this.currentPdfDoc.original_name}`;
        const newDocRes = await this.fetchAPI('/api/rems', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content: title, parent_id: null })
        });
        if (newDocRes && newDocRes.ok) {
          const newDoc = await newDocRes.json();
          targetDocID = newDoc.id;
          parentID = newDoc.id;
          afterID = null;
          await this.loadDocuments();
        }
      }

      // Insert rem bullet
      const payload = {
        content: remContent,
        parent_id: parentID,
      };
      if (afterID) {
        payload.after_id = afterID;
      }

      const insertRes = await this.fetchAPI('/api/rems', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });

      if (insertRes && insertRes.ok) {
        const createdRem = await insertRes.json();
        if (this.isPdfFullscreen) {
          this.togglePdfFullscreen();
        }
        await this.zoomTo(targetDocID);

        setTimeout(() => {
          const el = document.querySelector(`[data-id="${createdRem.id}"]`);
          if (el) {
            el.scrollIntoView({ behavior: 'smooth', block: 'center' });
            el.classList.add('bullet-pulse');
            setTimeout(() => el.classList.remove('bullet-pulse'), 1500);
          }
        }, 120);

        this.clearPdfSelection();
        this.showToast('Excerpt extracted to notes!');
      }
    } catch (e) {
      console.error('Failed to extract excerpt to outline', e);
    }
  }

  async createCardFromSelection() {
    if (!this.activePdfSelection || !this.activePdfSelection.docId) return;
    const { text, pageNum, rects, docId } = this.activePdfSelection;

    try {
      const hlRes = await this.fetchAPI(`/api/pdfs/${docId}/highlights`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          page_number: pageNum,
          rects_json: JSON.stringify(rects),
          text_content: text,
          color: this.activeHighlightColor || '#ffeb3b'
        })
      });
      if (!hlRes || !hlRes.ok) return;
      const hl = await hlRes.json();
      this.pdfHighlights.push(hl);

      const pageWrapper = document.getElementById(`pdf-page-${pageNum}`);
      if (pageWrapper) {
        this.renderHighlightsForPage(pageNum, pageWrapper.querySelector('.pdf-highlight-layer'));
      }
      this.updateHighlightsDrawer();

      const pinRef = `[[pdf:${docId}#p=${pageNum}&h=${hl.id}|p.${pageNum}]]`;
      const remContent = `${text} :: 📌 ${pinRef}`;

      let targetDocID = this.currentDocID;
      let parentID = this.lastFocusedParentID || targetDocID;
      let afterID = this.lastFocusedRemID || null;

      if (!targetDocID) {
        const title = `Notes on ${this.currentPdfDoc.original_name}`;
        const newDocRes = await this.fetchAPI('/api/rems', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content: title, parent_id: null })
        });
        if (newDocRes && newDocRes.ok) {
          const newDoc = await newDocRes.json();
          targetDocID = newDoc.id;
          parentID = newDoc.id;
          afterID = null;
          await this.loadDocuments();
        }
      }

      const payload = {
        content: remContent,
        parent_id: parentID,
      };
      if (afterID) payload.after_id = afterID;

      const res = await this.fetchAPI('/api/rems', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });

      if (res && res.ok) {
        const createdRem = await res.json();
        if (this.isPdfFullscreen) this.togglePdfFullscreen();
        await this.zoomTo(targetDocID);
        this.clearPdfSelection();
        this.showToast('Flashcard created from excerpt!');
        this.focusEditorByID(createdRem.id);
      }
    } catch (e) {
      console.error('Failed to create card from excerpt', e);
    }
  }

  async copySelectionAsBullet() {
    if (!this.activePdfSelection || !this.activePdfSelection.docId) return;
    const { text, pageNum, rects, docId } = this.activePdfSelection;

    try {
      const hlRes = await this.fetchAPI(`/api/pdfs/${docId}/highlights`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          page_number: pageNum,
          rects_json: JSON.stringify(rects),
          text_content: text,
          color: this.activeHighlightColor || '#ffeb3b'
        })
      });
      if (!hlRes || !hlRes.ok) return;
      const hl = await hlRes.json();
      this.pdfHighlights.push(hl);

      const pageWrapper = document.getElementById(`pdf-page-${pageNum}`);
      if (pageWrapper) {
        this.renderHighlightsForPage(pageNum, pageWrapper.querySelector('.pdf-highlight-layer'));
      }
      this.updateHighlightsDrawer();

      const pinRef = `[[pdf:${docId}#p=${pageNum}&h=${hl.id}|p.${pageNum}]]`;
      const remContent = `"${text}" 📌 ${pinRef}`;

      navigator.clipboard.writeText(remContent).then(() => {
        this.clearPdfSelection();
        this.showToast('Copied Rem bullet to clipboard');
      });
    } catch (e) {
      console.error('Failed to copy selection', e);
    }
  }

  // ========================================================
  // Split Resizer & Toast UI
  // ========================================================

  initSplitResizer() {
    const resizer = document.getElementById('splitResizer');
    const wrapper = document.getElementById('outlinerSplitWrapper');
    const outliner = document.getElementById('outlinerView');
    const pdfPane = document.getElementById('pdfReaderPane');
    if (!resizer || !wrapper || !outliner || !pdfPane) return;

    let isResizing = false;

    resizer.addEventListener('mousedown', () => {
      isResizing = true;
      resizer.classList.add('resizing');
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';
    });

    document.addEventListener('mousemove', (e) => {
      if (!isResizing) return;
      const wrapperRect = wrapper.getBoundingClientRect();
      const relativeX = e.clientX - wrapperRect.left;
      const totalWidth = wrapperRect.width;

      const leftPercent = Math.max(25, Math.min(75, (relativeX / totalWidth) * 100));
      const rightPercent = 100 - leftPercent;

      outliner.style.flex = `0 0 ${leftPercent}%`;
      pdfPane.style.flex = `0 0 ${rightPercent}%`;
    });

    document.addEventListener('mouseup', () => {
      if (isResizing) {
        isResizing = false;
        resizer.classList.remove('resizing');
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
      }
    });
  }

  showToast(message) {
    let toast = document.getElementById('remgoToast');
    if (!toast) {
      toast = document.createElement('div');
      toast.id = 'remgoToast';
      toast.style.cssText = `
        position: fixed;
        bottom: 24px;
        right: 24px;
        background: var(--bg-secondary);
        color: var(--text-primary);
        border: 1px solid var(--accent);
        border-radius: var(--radius);
        padding: 8px 16px;
        font-size: 0.85rem;
        box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
        z-index: 10000;
        opacity: 0;
        transform: translateY(12px);
        transition: opacity 0.2s ease, transform 0.2s ease;
        pointer-events: none;
      `;
      document.body.appendChild(toast);
    }
    toast.innerText = message;
    toast.style.opacity = '1';
    toast.style.transform = 'translateY(0)';
    clearTimeout(this._toastTimeout);
    this._toastTimeout = setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transform = 'translateY(12px)';
    }, 2500);
  }
}

// Instantiate and attach globally
const app = new RemGoApp();
window.app = app;
