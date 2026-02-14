// Mail Archive — Main Application (Vue 3)
// No TypeScript, no CDN — all resources are local.
// Template is loaded from main.template.vue at startup (fetched as raw HTML).

(async () => {
  'use strict';

  const KB = 1024;
  const MB = KB * KB;
  const EMAIL_CARD_EST_HEIGHT = 100;
  const MAX_PLACEHOLDER_CARDS = 50;
  const MAX_SPACER_HEIGHT = 50000;
  const VIRTUAL_WINDOW_BUFFER = 5;

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
        importPollTimer: null,
        loadingMore: false,
        isMobile: false,
        scrollY: 0,
        scrollBarTrackHeight: 0,
        scrollBarMaxScroll: 1,
        scrollBarDragging: false,
        viewportHeight: typeof window !== 'undefined' ? window.innerHeight : 800,
        listTop: 0
      };
    },

    computed: {
      totalPages() {
        if (!this.searchResults) return 0;
        return Math.ceil(this.searchResults.total / this.pageSize);
      },

      searchLoadProgress() {
        if (!this.searchResults?.total || this.searchResults.total === 0) return 0;
        const loaded = this.searchResults.hits?.length ?? 0;
        return Math.min(100, (loaded / this.searchResults.total) * 100);
      },

      displayItems() {
        if (!this.searchResults?.total) return [];
        const hits = this.searchResults.hits ?? [];
        const total = this.searchResults.total;
        const remaining = total - hits.length;
        const items = [];
        for (let i = 0; i < hits.length; i++) items.push({ type: 'hit', hit: hits[i] });
        const placeholdersToShow = Math.min(remaining, MAX_PLACEHOLDER_CARDS);
        for (let i = 0; i < placeholdersToShow; i++) items.push({ type: 'placeholder', index: hits.length + i });
        return items;
      },

      visibleItems() {
        const items = this.displayItems;
        if (items.length === 0) return [];
        const h = EMAIL_CARD_EST_HEIGHT;
        const vh = this.viewportHeight || 800;
        const scrollInList = Math.max(0, this.scrollY - this.listTop);
        const start = Math.max(0, Math.floor(scrollInList / h) - VIRTUAL_WINDOW_BUFFER);
        const count = Math.ceil(vh / h) + 2 * VIRTUAL_WINDOW_BUFFER;
        const end = Math.min(items.length, start + count);
        return items.slice(start, end);
      },

      virtualTopHeight() {
        const items = this.displayItems;
        if (items.length === 0) return 0;
        const h = EMAIL_CARD_EST_HEIGHT;
        const scrollInList = Math.max(0, this.scrollY - this.listTop);
        const start = Math.max(0, Math.floor(scrollInList / h) - VIRTUAL_WINDOW_BUFFER);
        const count = Math.ceil((this.viewportHeight || 800) / h) + 2 * VIRTUAL_WINDOW_BUFFER;
        const end = Math.min(items.length, start + count);
        const visibleStart = Math.min(start, items.length);
        return visibleStart * h;
      },

      virtualBottomHeight() {
        const items = this.displayItems;
        if (items.length === 0) return 0;
        const h = EMAIL_CARD_EST_HEIGHT;
        const scrollInList = Math.max(0, this.scrollY - this.listTop);
        const start = Math.max(0, Math.floor(scrollInList / h) - VIRTUAL_WINDOW_BUFFER);
        const count = Math.ceil((this.viewportHeight || 800) / h) + 2 * VIRTUAL_WINDOW_BUFFER;
        const end = Math.min(items.length, start + count);
        return (items.length - end) * h;
      },

      spacerHeight() {
        if (!this.searchResults?.total) return 0;
        const hits = this.searchResults.hits ?? [];
        const remaining = Math.max(0, this.searchResults.total - hits.length);
        if (remaining <= MAX_PLACEHOLDER_CARDS) return 0;
        const raw = (remaining - MAX_PLACEHOLDER_CARDS) * EMAIL_CARD_EST_HEIGHT;
        return Math.min(raw, MAX_SPACER_HEIGHT);
      },

      placeholderHeight() {
        if (!this.searchResults?.total) return 0;
        const loaded = this.searchResults.hits?.length ?? 0;
        const remaining = Math.max(0, this.searchResults.total - loaded);
        return remaining * EMAIL_CARD_EST_HEIGHT;
      },

      scrollBarThumbStyle() {
        const trackH = this.scrollBarTrackHeight || 300;
        const maxScroll = Math.max(1, this.scrollBarMaxScroll);
        const thumbH = Math.max(24, Math.min(trackH * 0.3, trackH * 0.4));
        const range = Math.max(0, trackH - thumbH);
        const pct = range > 0 ? Math.min(1, this.scrollY / maxScroll) : 0;
        return {
          height: thumbH + 'px',
          top: (pct * range) + 'px'
        };
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
      },

      detailHitIndex() {
        if (!this.selectedEmail || !this.searchResults?.hits?.length) return -1;
        const idx = this.searchResults.hits.findIndex(
          (h) => h.path === this.selectedEmail.path && (h.account_id || null) === (this.detailAccountId || null)
        );
        return idx;
      },

      detailPrevHit() {
        const idx = this.detailHitIndex;
        return idx > 0 ? this.searchResults.hits[idx - 1] : null;
      },

      detailNextHit() {
        const idx = this.detailHitIndex;
        const hits = this.searchResults?.hits ?? [];
        return idx >= 0 && idx < hits.length - 1 ? hits[idx + 1] : null;
      },

      detailCountDisplay() {
        const idx = this.detailHitIndex;
        const total = this.searchResults?.total ?? this.searchResults?.hits?.length ?? 0;
        if (idx < 0 || total === 0) return '';
        return `${idx + 1} of ${total}`;
      }
    },

    created() {
      this.isMobile = typeof window !== 'undefined' && window.innerWidth < 768;
      this.loadUser();
      this.loadAccounts();
      this.doSearch('', 0);
      this.startSyncPoll();

      window.addEventListener('hashchange', () => this.handleRoute());
      this.handleRoute();
    },

    mounted() {
      this.checkMobile = () => {
        this.isMobile = window.innerWidth < 768;
        this.viewportHeight = window.innerHeight;
        this.measureScrollBar();
      };
      window.addEventListener('resize', this.checkMobile);
      window.addEventListener('scroll', this.onScrollThrottled, { passive: true });
      this.$nextTick(() => {
        this.onScroll();
        this.measureScrollBar();
      });
    },

    updated() {
      this.setupLoadMoreObserver();
    },

    beforeUnmount() {
      window.removeEventListener('resize', this.checkMobile);
      window.removeEventListener('scroll', this.onScrollThrottled);
      if (this._scrollBarMouseMove) window.removeEventListener('mousemove', this._scrollBarMouseMove);
      if (this._scrollBarMouseUp) window.removeEventListener('mouseup', this._scrollBarMouseUp);
      if (this._scrollBarTouchMove) window.removeEventListener('touchmove', this._scrollBarTouchMove);
      if (this._scrollBarTouchEnd) {
        window.removeEventListener('touchend', this._scrollBarTouchEnd);
        window.removeEventListener('touchcancel', this._scrollBarTouchEnd);
      }
      this._loadMoreObserver?.disconnect();
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

      async doSearch(query, offset, append = false) {
        const off = offset ?? 0;
        let url = `/api/search?limit=${this.pageSize}&offset=${off}&q=${encodeURIComponent(query || '')}`;
        const ids = this.enabledSearchAccountIds();
        if (ids.length > 0 && ids.length < this.accounts.length) url += `&account_ids=${encodeURIComponent(ids.join(','))}`;
        if (this.searchMode === 'similarity') url += '&mode=similarity';

        if (append) {
          this.loadingMore = true;
        } else {
          this.searchResults = { total: this.pageSize, hits: [], query };
          this._lastLoadTime = 0;
        }

        try {
          const r = await fetch(url);
          const data = r.ok ? await r.json() : { total: 0, hits: [] };
          if (append && this.searchResults?.hits?.length) {
            this.searchResults = { ...this.searchResults, ...data, hits: [...this.searchResults.hits, ...(data.hits || [])] };
            this.currentPage = Math.floor(off / this.pageSize);
          } else {
            this.searchResults = { ...data, hits: data.hits || [] };
            this.currentPage = Math.floor(off / this.pageSize);
            if (!append) this._loadMoreObserverSetup = false;
          }
        } catch {
          if (!append) this.searchResults = { total: 0, hits: [], query };
        } finally {
          if (append) this.loadingMore = false;
          this.$nextTick(() => {
            this.onScroll();
            this.measureScrollBar();
          });
        }
      },

      loadMore() {
        if (this.loadingMore || !this.searchResults) return;
        if (this.searchResults.hits?.length === 0) return;
        if (this.currentPage >= this.totalPages - 1) return;
        this.doSearch(this.searchQuery, (this.currentPage + 1) * this.pageSize, true);
      },

      setupLoadMoreObserver() {
        if (this.view !== 'search') return;
        const sentinel = this.$refs.loadMoreSentinel;
        if (!sentinel) {
          this._loadMoreObserverSetup = false;
          return;
        }
        if (this._loadMoreObserverSetup) return;
        this.$nextTick(() => {
          const el = this.$refs.loadMoreSentinel;
          if (!el) return;
          this._loadMoreObserver?.disconnect();
          const LOAD_COOLDOWN_MS = 800;
          this._loadMoreObserver = new IntersectionObserver(
            (entries) => {
              if (!entries[0]?.isIntersecting) return;
              const now = Date.now();
              if (this._lastLoadTime && now - this._lastLoadTime < LOAD_COOLDOWN_MS) return;
              this._lastLoadTime = now;
              this.loadMore();
            },
            { rootMargin: '200px', threshold: 0 }
          );
          this._loadMoreObserver.observe(el);
          this._loadMoreObserverSetup = true;
        });
      },

      onScroll() {
        if (this.view !== 'search') return;
        this.scrollY = window.scrollY ?? window.pageYOffset ?? 0;
        this.scrollBarMaxScroll = Math.max(1, (document.documentElement.scrollHeight - window.innerHeight));
        const listEl = this.$refs.searchResultsList;
        if (listEl) this.listTop = listEl.getBoundingClientRect().top + (window.scrollY ?? 0);
        this.loadPageAtScrollPosition(this.scrollY);
      },

      onScrollThrottled() {
        if (this._scrollRaf) return;
        this._scrollRaf = requestAnimationFrame(() => {
          this._scrollRaf = null;
          this.onScroll();
        });
      },

      measureScrollBar() {
        if (this.view !== 'search') return;
        this.$nextTick(() => {
          const track = this.$refs.scrollBarTrack;
          if (track) this.scrollBarTrackHeight = track.clientHeight;
          const listEl = this.$refs.searchResultsList;
          if (listEl) this.listTop = listEl.getBoundingClientRect().top + (window.scrollY ?? 0);
        });
      },

      scrollToPosition(pct, instant = false) {
        const maxScroll = Math.max(1, document.documentElement.scrollHeight - window.innerHeight);
        const targetY = Math.max(0, Math.min(maxScroll, pct * maxScroll));
        window.scrollTo({ top: targetY, behavior: instant ? 'auto' : 'smooth' });
        this.loadPageAtScrollPosition(targetY);
      },

      loadPageAtScrollPosition(scrollY) {
        if (!this.searchResults?.total || this.loadingMore) return;
        const maxScroll = Math.max(1, document.documentElement.scrollHeight - window.innerHeight);
        const pct = scrollY / maxScroll;
        const targetItemIndex = Math.floor(pct * this.searchResults.total);
        const targetPage = Math.min(this.totalPages - 1, Math.floor(targetItemIndex / this.pageSize));
        if (targetPage > this.currentPage) {
          this.doSearch(this.searchQuery, (this.currentPage + 1) * this.pageSize, true);
        }
      },

      onScrollBarTrackClick(e) {
        if (this.scrollBarDragging) return;
        const track = this.$refs.scrollBarTrack;
        if (!track) return;
        const rect = track.getBoundingClientRect();
        const clickY = e.clientY - rect.top;
        const trackH = track.clientHeight;
        const thumbStyle = this.scrollBarThumbStyle;
        const thumbH = parseInt(thumbStyle.height, 10);
        const range = trackH - thumbH;
        if (range <= 0) return;
        const pct = Math.min(1, Math.max(0, (clickY - thumbH / 2) / range));
        this.scrollToPosition(pct);
      },

      _scrollBarDragStart(clientY) {
        this.scrollBarDragging = true;
        const track = this.$refs.scrollBarTrack;
        const startY = clientY;
        const startScrollY = this.scrollY;
        const maxScroll = this.scrollBarMaxScroll;
        const trackH = track?.clientHeight || 300;
        const thumbH = parseInt(this.scrollBarThumbStyle.height, 10);
        const range = trackH - thumbH;

        const move = (ev) => {
          if (!this.scrollBarDragging) return;
          const y = ev.touches ? ev.touches[0].clientY : ev.clientY;
          const dy = y - startY;
          const pctChange = range > 0 ? dy / range : 0;
          const pct = Math.min(1, Math.max(0, startScrollY / maxScroll + pctChange));
          this.scrollToPosition(pct, true);
        };
        const end = () => {
          this.scrollBarDragging = false;
          window.removeEventListener('mousemove', this._scrollBarMouseMove);
          window.removeEventListener('mouseup', this._scrollBarMouseUp);
          window.removeEventListener('touchmove', this._scrollBarTouchMove, { passive: false });
          window.removeEventListener('touchend', this._scrollBarTouchEnd);
          window.removeEventListener('touchcancel', this._scrollBarTouchEnd);
          this._scrollBarMouseMove = null;
          this._scrollBarMouseUp = null;
          this._scrollBarTouchMove = null;
          this._scrollBarTouchEnd = null;
        };

        this._scrollBarMouseMove = move;
        this._scrollBarMouseUp = end;
        this._scrollBarTouchMove = (e) => { e.preventDefault(); move(e); };
        this._scrollBarTouchEnd = end;

        window.addEventListener('mousemove', this._scrollBarMouseMove);
        window.addEventListener('mouseup', this._scrollBarMouseUp);
        window.addEventListener('touchmove', this._scrollBarTouchMove, { passive: false });
        window.addEventListener('touchend', this._scrollBarTouchEnd);
        window.addEventListener('touchcancel', this._scrollBarTouchEnd);
      },

      onScrollBarThumbMouseDown(e) {
        e.preventDefault();
        this._scrollBarDragStart(e.clientY);
      },

      onScrollBarThumbTouchStart(e) {
        if (e.touches.length) this._scrollBarDragStart(e.touches[0].clientY);
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

      folderFromPath(path) {
        if (!path) return '';
        const parts = path.split('/').filter(Boolean);
        return parts.length >= 2 ? parts[parts.length - 2] : '';
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
        this.navigate('#');
      },

      goToEmail(hit) {
        this.navigate(this.emailDetailHref(hit));
      },

      onDetailTouchStart(e) {
        if (e.changedTouches?.length) this._touchStartX = e.changedTouches[0].clientX;
      },

      onDetailTouchEnd(e) {
        if (!e.changedTouches?.length || this._touchStartX == null) return;
        const dx = e.changedTouches[0].clientX - this._touchStartX;
        this._touchStartX = null;
        const threshold = 60;
        if (dx > threshold && this.detailPrevHit) this.goToEmail(this.detailPrevHit);
        else if (dx < -threshold && this.detailNextHit) this.goToEmail(this.detailNextHit);
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

    }
  };

  Vue.createApp(App).mount('#app');
})();
