// Mail Archive — Main Application (Vue 3)
// No TypeScript, no CDN — all resources are local.
// Template is loaded from main.template.vue at startup (fetched as raw HTML).

(async () => {
  'use strict';

  const KB = 1024;
  const MB = KB * KB;

  const templateResponse = await fetch('/static/js/app/main.template.vue');
  const templateHTML = await templateResponse.text();

  const App = {
    template: templateHTML,

    data() {
      return {
        view: 'search',   // 'search' | 'accounts' | 'sync' | 'detail'
        user: null,
        accounts: [],
        searchQuery: '',
        searchResults: null,
        searchMode: 'keyword',
        searchAccountMask: {},
        currentPage: 0,
        pageSize: 50,
        selectedEmail: null,
        detailAccountId: null,
        loading: false,
        syncStatuses: [],
        syncStatusMap: {},
        accountPollTimer: null,
        showAddAccount: false,
        editingAccount: null,
        newAccount: {
          type: 'IMAP',
          email: '',
          host: '',
          port: 993,
          password: '',
          ssl: true,
          folders: 'all',
          sync: { interval: '5m', enabled: true }
        },
        toasts: [],
        debounceTimer: null,
        importTitle: '',
        importFile: null,
        importRunning: false,
        importJob: null,
        importHistory: [],
        importPollTimer: null
      };
    },

    computed: {
      totalPages() {
        if (!this.searchResults) return 0;
        return Math.ceil(this.searchResults.total / this.pageSize);
      },

      importPhaseLabel() {
        if (!this.importJob) return '';
        const labels = {
          uploading: 'Uploading...',
          extracting: 'Extracting messages...',
          indexing: 'Building search index...',
          done: 'Import complete',
          error: 'Import failed'
        };
        return labels[this.importJob.phase] || this.importJob.phase;
      },

      importProgressDetail() {
        if (!this.importJob) return '';
        const { phase, current, total } = this.importJob;
        let detail;
        switch (phase) {
          case 'uploading': detail = total > 0 ? `${current} / ${total} MB` : ''; break;
          case 'extracting': detail = `${current} messages`; break;
          case 'done': detail = `${current} messages imported`; break;
          default: detail = '';
        }
        return detail;
      },

      importPercent() {
        if (!this.importJob) return 0;
        const { phase, current, total } = this.importJob;
        let pct;
        switch (phase) {
          case 'done': pct = 100; break;
          case 'error': pct = 0; break;
          case 'uploading': pct = total > 0 ? Math.min(99, Math.round(current / total * 100)) : 0; break;
          case 'extracting': pct = current > 0 ? 50 : 0; break;
          case 'indexing': pct = 90; break;
          default: pct = 0;
        }
        return pct;
      }
    },

    created() {
      this.loadUser();
      this.loadAccounts();
      this.doSearch('', 0);
      this.startSyncPoll();

      window.addEventListener('hashchange', () => this.handleRoute());
      this.handleRoute();
    },

    methods: {
      // --- Routing ---
      handleRoute() {
        const hash = location.hash || '#';
        if (hash.startsWith('#/email/')) {
          const rest = hash.slice(8);
          const qIdx = rest.indexOf('?');
          const emailPath = decodeURIComponent(qIdx >= 0 ? rest.slice(0, qIdx) : rest);
          const accountId = qIdx >= 0 ? new URLSearchParams(rest.slice(qIdx)).get('account_id') : null;
          this.showEmailDetail(emailPath, accountId);
          return;
        }
        switch (hash) {
          case '#/accounts': this.view = 'accounts'; this.loadSyncStatus(); break;
          case '#/import': this.view = 'import'; break;
          default: this.view = 'search';
        }
      },

      navigate(hash) {
        if (location.hash !== hash) location.hash = hash;
        else this.handleRoute();
      },

      // --- User ---
      async loadUser() {
        try {
          const r = await fetch('/api/me');
          if (!r.ok) throw new Error();
          this.user = await r.json();
        } catch {
          window.location.href = '/login';
        }
      },

      async logout() {
        try {
          await fetch('/logout', { method: 'POST' });
        } finally {
          window.location.href = '/login';
        }
      },

      // --- Accounts ---
      async loadAccounts() {
        try {
          const r = await fetch('/api/accounts');
          this.accounts = r.ok ? (await r.json()) || [] : [];
        } catch {
          this.accounts = [];
        }
      },

      openAddAccount() {
        this.editingAccount = null;
        this.newAccount = {
          type: 'IMAP',
          email: '',
          host: '',
          port: 993,
          password: '',
          ssl: true,
          folders: 'all',
          sync: { interval: '5m', enabled: true }
        };
        this.showAddAccount = true;
      },

      openEditAccount(acct) {
        this.editingAccount = acct.id;
        this.newAccount = JSON.parse(JSON.stringify(acct));
        this.showAddAccount = true;
      },

      async saveAccount() {
        const url = this.editingAccount ? `/api/accounts/${this.editingAccount}` : '/api/accounts';
        const method = this.editingAccount ? 'PUT' : 'POST';
        try {
          const r = await fetch(url, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(this.newAccount)
          });
          if (!r.ok) throw new Error();
          this.showAddAccount = false;
          this.loadAccounts();
          this.showToast(this.editingAccount ? 'Account updated' : 'Account added', 'success');
        } catch {
          this.showToast(this.editingAccount ? 'Failed to update account' : 'Failed to add account', 'error');
        }
      },

      async deleteAccount(acct) {
        if (!confirm(`Delete account ${acct.email}? Downloaded emails will NOT be removed.`)) return;
        try {
          const r = await fetch(`/api/accounts/${acct.id}`, { method: 'DELETE' });
          if (!r.ok) throw new Error();
          this.loadAccounts();
          this.showToast('Account deleted', 'success');
        } catch {
          this.showToast('Failed to delete account', 'error');
        }
      },

      updatePortForType() {
        const defaults = { IMAP: 993, POP3: 995, GMAIL_API: 0 };
        this.newAccount.port = defaults[this.newAccount.type] || 993;
      },

      accountTypeBadge(type) {
        return `badge badge-${type.toLowerCase().replace('_', '')}`;
      },

      accountIcon(type) {
        const icons = { IMAP: '\u{1F4E8}', POP3: '\u{1F4EC}', GMAIL_API: '\u{1F4E7}', PST: '\u{1F4C2}' };
        return icons[type] ?? '\u{1F4E7}';
      },

      // --- Search ---
      onSearchInput() {
        clearTimeout(this.debounceTimer);
        this.currentPage = 0;
        this.debounceTimer = setTimeout(() => this.doSearch(this.searchQuery, 0), 200);
      },

      async doSearch(query, offset) {
        let url = `/api/search?limit=${this.pageSize}&offset=${offset || 0}&q=${encodeURIComponent(query || '')}`;
        const ids = this.enabledSearchAccountIds();
        if (ids.length > 0 && ids.length < this.accounts.length) url += `&account_ids=${encodeURIComponent(ids.join(','))}`;
        if (this.searchMode === 'similarity') url += '&mode=similarity';

        try {
          const r = await fetch(url);
          const data = r.ok ? await r.json() : { total: 0, hits: [] };
          this.searchResults = { ...data, hits: data.hits || [] };
        } catch {
          this.searchResults = { total: 0, hits: [], query };
        }
      },

      goToPage(page) {
        this.currentPage = page;
        this.doSearch(this.searchQuery, page * this.pageSize);
        window.scrollTo({ top: 0, behavior: 'smooth' });
      },

      setSearchMode(mode) {
        this.searchMode = mode;
        this.currentPage = 0;
        this.doSearch(this.searchQuery, 0);
      },

      isSearchAccountEnabled(acctId) {
        return this.searchAccountMask[acctId] !== false;
      },

      toggleSearchAccount(acctId) {
        this.searchAccountMask = { ...this.searchAccountMask, [acctId]: !this.isSearchAccountEnabled(acctId) };
        this.currentPage = 0;
        this.doSearch(this.searchQuery, 0);
      },

      enabledSearchAccountIds() {
        return this.accounts.filter((a) => this.isSearchAccountEnabled(a.id)).map((a) => a.id);
      },

      emailDetailHref(hit) {
        let h = `#/email/${encodeURIComponent(hit.path)}`;
        if (hit.account_id) h += `?account_id=${encodeURIComponent(hit.account_id)}`;
        return h;
      },

      highlightText(text, query) {
        if (!query || !text) return this.escapeHtml(text || '');
        const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        const re = new RegExp(`(${escaped})`, 'gi');
        return this.escapeHtml(text).replace(re, '<mark>$1</mark>');
      },

      formatDate(dateStr) {
        if (!dateStr) return '';
        const d = new Date(dateStr);
        return `${d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })} ${d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })}`;
      },

      // --- Email Detail ---
      async showEmailDetail(path, accountId) {
        this.view = 'detail';
        this.loading = true;
        this.selectedEmail = null;
        this.detailAccountId = accountId ?? null;

        let url = `/api/email?path=${encodeURIComponent(path)}`;
        if (accountId) url += `&account_id=${encodeURIComponent(accountId)}`;

        try {
          const r = await fetch(url);
          if (!r.ok) throw new Error();
          const data = await r.json();
          this.selectedEmail = data;
          this.loading = false;
          this.$nextTick(() => {
            const iframe = document.getElementById('email-iframe');
            if (iframe && data.html_body) {
              iframe.srcdoc = data.html_body;
              iframe.addEventListener('load', () => {
                try {
                  const h = iframe.contentDocument.documentElement.scrollHeight;
                  iframe.style.height = `${Math.max(h + 20, 200)}px`;
                } catch (e) { /* cross-origin */ }
              });
            }
          });
        } catch {
          this.loading = false;
          this.showToast('Failed to load email', 'error');
        }
      },

      goBack() {
        history.back();
      },

      emailDownloadUrl() {
        if (!this.selectedEmail?.path) return '#';
        let url = `/api/email/download?path=${encodeURIComponent(this.selectedEmail.path)}`;
        if (this.detailAccountId) url += `&account_id=${encodeURIComponent(this.detailAccountId)}`;
        return url;
      },

      attachmentDownloadUrl(index) {
        if (!this.selectedEmail?.path) return '#';
        let url = `/api/email/attachment?path=${encodeURIComponent(this.selectedEmail.path)}&index=${index}`;
        if (this.detailAccountId) url += `&account_id=${encodeURIComponent(this.detailAccountId)}`;
        return url;
      },

      // --- Sync ---
      async fetchAndApplySyncStatus(onApplied) {
        try {
          const r = await fetch('/api/sync/status');
          const list = r.ok ? (await r.json()) || [] : [];
          this.syncStatuses = list;
          this.syncStatusMap = Object.fromEntries(list.map((s) => [s.id, s]));

          list.forEach((s) => {
            if (s.last_error && !s.syncing) {
              const key = `sync_error_${s.id}`;
              this._shownErrors ??= {};
              if (!this._shownErrors[key]) {
                this._shownErrors[key] = true;
                this.showToast(`${s.name}: ${s.last_error}`, 'error');
              }
            } else if (this._shownErrors) delete this._shownErrors[`sync_error_${s.id}`];
          });
          onApplied?.(list);
        } catch {
          this.syncStatuses = [];
        }
      },

      loadSyncStatus() {
        this.fetchAndApplySyncStatus();
      },

      async triggerSync(accountID) {
        try {
          const r = await fetch('/api/sync', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(accountID ? { account_id: accountID } : {})
          });
          switch (r.status) {
            case 409: this.showToast('Sync already running', 'warning'); break;
            case 200:
            case 201:
            case 204: this.showToast('Sync started', 'success'); this.pollSyncStatus(); break;
            default: throw new Error();
          }
        } catch {
          this.showToast('Sync failed', 'error');
        }
      },

      async stopSync(accountID) {
        try {
          const r = await fetch('/api/sync/stop', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ account_id: accountID })
          });
          if (!r.ok) throw new Error();
          this.showToast('Sync stopped', 'warning');
          this.refreshSyncStatus();
        } catch {
          this.showToast('Failed to stop sync', 'error');
        }
      },

      accountSyncStatus(accountID) {
        return this.syncStatusMap[accountID] ?? {};
      },

      startSyncPoll() {
        this.refreshSyncStatus();
        this.accountPollTimer = setInterval(() => this.refreshSyncStatus(), 3000);
      },

      refreshSyncStatus() {
        this.fetchAndApplySyncStatus();
      },

      pollSyncStatus() {
        const poll = setInterval(() => {
          this.fetchAndApplySyncStatus((list) => {
            if (!list.some((s) => s.syncing)) {
              clearInterval(poll);
              this.showToast('Sync complete', 'success');
            }
          });
        }, 2000);
      },

      // --- Toasts ---
      showToast(message, type = 'success') {
        const toast = { id: Date.now(), message, type };
        this.toasts.push(toast);
        setTimeout(() => {
          this.toasts = this.toasts.filter((t) => t.id !== toast.id);
        }, 4000);
      },

      // --- Helpers ---
      escapeHtml(s) {
        if (!s) return '';
        const div = document.createElement('div');
        div.textContent = s;
        return div.innerHTML;
      },

      formatSize(bytes) {
        if (!bytes) return '';
        let value, unit;
        switch (true) {
          case bytes >= MB: value = (bytes / MB).toFixed(1); unit = 'MB'; break;
          case bytes >= KB: value = (bytes / KB).toFixed(1); unit = 'KB'; break;
          default: value = String(bytes); unit = 'B';
        }
        return `${value} ${unit}`;
      },

      // --- PST/OST Import ---
      onPSTFileSelected(e) {
        this.importFile = e.target.files[0] ?? null;
        if (this.importFile && !this.importTitle) {
          this.importTitle = this.importFile.name.replace(/\.(pst|ost)$/i, '');
        }
      },

      startPSTImport() {
        if (!this.importFile) return;
        this.importRunning = true;
        this.importJob = { phase: 'uploading', current: 0, total: 0 };

        const formData = new FormData();
        formData.append('file', this.importFile);
        formData.append('title', this.importTitle || this.importFile.name);

        const xhr = new XMLHttpRequest();
        xhr.upload.addEventListener('progress', (e) => {
          if (e.lengthComputable) {
            this.importJob = {
              phase: 'uploading',
              current: Math.round(e.loaded / MB),
              total: Math.round(e.total / MB)
            };
          }
        });
        xhr.addEventListener('load', () => {
          if (xhr.status >= 200 && xhr.status < 300) {
            const data = JSON.parse(xhr.responseText);
            this.importJob = { id: data.job_id, phase: 'extracting', current: 0, total: 0 };
            this.pollImportStatus(data.job_id);
            this.showToast('Upload complete, extracting messages...', 'success');
          } else {
            this.importRunning = false;
            let msg = 'Upload failed';
            try { msg = JSON.parse(xhr.responseText).error; } catch (e) {}
            this.importJob = { phase: 'error', error: msg };
            this.showToast(msg, 'error');
          }
        });
        xhr.addEventListener('error', () => {
          this.importRunning = false;
          this.importJob = { phase: 'error', error: 'Upload failed' };
          this.showToast('Upload failed', 'error');
        });
        xhr.open('POST', '/api/import/pst');
        xhr.send(formData);
      },

      pollImportStatus(jobID) {
        if (this.importPollTimer) clearInterval(this.importPollTimer);
        this.importPollTimer = setInterval(async () => {
          try {
            const r = await fetch(`/api/import/status/${jobID}`);
            if (!r.ok) return;
            const data = await r.json();
            this.importJob = data;
            const finish = () => {
              clearInterval(this.importPollTimer);
              this.importPollTimer = null;
              this.importRunning = false;
              this.importHistory.unshift(data);
            };
            switch (data.phase) {
              case 'done': finish(); this.loadAccounts(); this.showToast(`Import complete: ${data.current} messages`, 'success'); break;
              case 'error': finish(); this.showToast(`Import failed: ${data.error}`, 'error'); break;
            }
          } catch {
            // ignore poll errors
          }
        }, 1500);
      },

      pagerRange() {
        const total = this.totalPages;
        const current = this.currentPage;
        const start = Math.max(0, current - 2);
        const end = Math.min(total - 1, current + 2);
        const pages = [];

        if (start > 0) pages.push({ num: 0, label: '1' });
        if (start > 1) pages.push({ num: -1, label: '...' });
        for (let i = start; i <= end; i++) pages.push({ num: i, label: String(i + 1) });
        if (end < total - 2) pages.push({ num: -1, label: '...' });
        if (end < total - 1) pages.push({ num: total - 1, label: String(total) });

        return pages;
      }
    }
  };

  Vue.createApp(App).mount('#app');
})();
