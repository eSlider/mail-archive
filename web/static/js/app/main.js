// Mail Archive — Main Application (Vue 3 + jQuery)
// No TypeScript, no CDN — all resources are local.

var App = {
  data: function () {
    return {
      view: 'search',   // 'search' | 'accounts' | 'sync' | 'detail'
      user: null,
      accounts: [],
      searchQuery: '',
      searchResults: null,
      searchMode: 'keyword',
      currentPage: 0,
      pageSize: 50,
      selectedEmail: null,
      loading: false,
      syncStatuses: [],
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
      debounceTimer: null
    }
  },

  computed: {
    totalPages: function () {
      if (!this.searchResults) return 0
      return Math.ceil(this.searchResults.total / this.pageSize)
    }
  },

  created: function () {
    this.loadUser()
    this.loadAccounts()
    this.doSearch('', 0)

    // Handle browser back/forward.
    var self = this
    window.addEventListener('hashchange', function () {
      self.handleRoute()
    })
    this.handleRoute()
  },

  methods: {
    // --- Routing ---
    handleRoute: function () {
      var hash = location.hash || '#'
      if (hash.indexOf('#/email/') === 0) {
        var emailPath = decodeURIComponent(hash.slice(8))
        this.showEmailDetail(emailPath)
      } else if (hash === '#/accounts') {
        this.view = 'accounts'
      } else if (hash === '#/sync') {
        this.view = 'sync'
        this.loadSyncStatus()
      } else {
        this.view = 'search'
      }
    },

    navigate: function (hash) {
      if (location.hash !== hash) {
        location.hash = hash
      } else {
        this.handleRoute()
      }
    },

    // --- User ---
    loadUser: function () {
      var self = this
      $.getJSON('/api/me').done(function (data) {
        self.user = data
      }).fail(function () {
        window.location.href = '/login'
      })
    },

    logout: function () {
      $.post('/logout').always(function () {
        window.location.href = '/login'
      })
    },

    // --- Accounts ---
    loadAccounts: function () {
      var self = this
      $.getJSON('/api/accounts').done(function (data) {
        self.accounts = data || []
      })
    },

    openAddAccount: function () {
      this.editingAccount = null
      this.newAccount = {
        type: 'IMAP',
        email: '',
        host: '',
        port: 993,
        password: '',
        ssl: true,
        folders: 'all',
        sync: { interval: '5m', enabled: true }
      }
      this.showAddAccount = true
    },

    openEditAccount: function (acct) {
      this.editingAccount = acct.id
      this.newAccount = JSON.parse(JSON.stringify(acct))
      this.showAddAccount = true
    },

    saveAccount: function () {
      var self = this
      if (this.editingAccount) {
        $.ajax({
          url: '/api/accounts/' + this.editingAccount,
          type: 'PUT',
          contentType: 'application/json',
          data: JSON.stringify(this.newAccount)
        }).done(function () {
          self.showAddAccount = false
          self.loadAccounts()
          self.showToast('Account updated', 'success')
        }).fail(function () {
          self.showToast('Failed to update account', 'error')
        })
      } else {
        $.ajax({
          url: '/api/accounts',
          type: 'POST',
          contentType: 'application/json',
          data: JSON.stringify(this.newAccount)
        }).done(function () {
          self.showAddAccount = false
          self.loadAccounts()
          self.showToast('Account added', 'success')
        }).fail(function () {
          self.showToast('Failed to add account', 'error')
        })
      }
    },

    deleteAccount: function (acct) {
      if (!confirm('Delete account ' + acct.email + '? Downloaded emails will NOT be removed.')) return
      var self = this
      $.ajax({
        url: '/api/accounts/' + acct.id,
        type: 'DELETE'
      }).done(function () {
        self.loadAccounts()
        self.showToast('Account deleted', 'success')
      })
    },

    updatePortForType: function () {
      var defaults = { IMAP: 993, POP3: 995, GMAIL_API: 0 }
      this.newAccount.port = defaults[this.newAccount.type] || 993
    },

    accountTypeBadge: function (type) {
      return 'badge badge-' + type.toLowerCase().replace('_', '')
    },

    accountIcon: function (type) {
      var icons = { IMAP: '📨', POP3: '📬', GMAIL_API: '📧' }
      return icons[type] || '📧'
    },

    // --- Search ---
    onSearchInput: function () {
      var self = this
      clearTimeout(this.debounceTimer)
      this.currentPage = 0
      this.debounceTimer = setTimeout(function () {
        self.doSearch(self.searchQuery, 0)
      }, 200)
    },

    doSearch: function (query, offset) {
      var self = this
      var url = '/api/search?limit=' + this.pageSize + '&offset=' + (offset || 0)
      url += '&q=' + encodeURIComponent(query || '')
      if (this.searchMode === 'similarity') url += '&mode=similarity'

      $.getJSON(url).done(function (data) {
        self.searchResults = data
        self.searchResults.hits = data.hits || []
      }).fail(function () {
        self.searchResults = { total: 0, hits: [], query: query }
      })
    },

    goToPage: function (page) {
      this.currentPage = page
      this.doSearch(this.searchQuery, page * this.pageSize)
      window.scrollTo({ top: 0, behavior: 'smooth' })
    },

    setSearchMode: function (mode) {
      this.searchMode = mode
      this.currentPage = 0
      this.doSearch(this.searchQuery, 0)
    },

    highlightText: function (text, query) {
      if (!query || !text) return this.escapeHtml(text || '')
      var escaped = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
      var re = new RegExp('(' + escaped + ')', 'gi')
      return this.escapeHtml(text).replace(re, '<mark>$1</mark>')
    },

    formatDate: function (dateStr) {
      if (!dateStr) return ''
      var d = new Date(dateStr)
      return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' }) +
        ' ' + d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })
    },

    // --- Email Detail ---
    showEmailDetail: function (path) {
      var self = this
      this.view = 'detail'
      this.loading = true
      this.selectedEmail = null

      $.getJSON('/api/email?path=' + encodeURIComponent(path)).done(function (data) {
        self.selectedEmail = data
        self.loading = false
        // Set iframe content after Vue renders.
        self.$nextTick(function () {
          var iframe = document.getElementById('email-iframe')
          if (iframe && data.html_body) {
            iframe.srcdoc = data.html_body
            iframe.addEventListener('load', function () {
              try {
                var h = iframe.contentDocument.documentElement.scrollHeight
                iframe.style.height = Math.max(h + 20, 200) + 'px'
              } catch (e) { /* cross-origin */ }
            })
          }
        })
      }).fail(function () {
        self.loading = false
        self.showToast('Failed to load email', 'error')
      })
    },

    goBack: function () {
      history.back()
    },

    // --- Sync ---
    loadSyncStatus: function () {
      var self = this
      $.getJSON('/api/sync/status').done(function (data) {
        self.syncStatuses = data || []
      })
    },

    triggerSync: function (accountID) {
      var self = this
      var body = accountID ? { account_id: accountID } : {}
      $.ajax({
        url: '/api/sync',
        type: 'POST',
        contentType: 'application/json',
        data: JSON.stringify(body)
      }).done(function () {
        self.showToast('Sync started', 'success')
        self.pollSyncStatus()
      }).fail(function (xhr) {
        if (xhr.status === 409) {
          self.showToast('Sync already running', 'warning')
        } else {
          self.showToast('Sync failed', 'error')
        }
      })
    },

    pollSyncStatus: function () {
      var self = this
      var poll = setInterval(function () {
        $.getJSON('/api/sync/status').done(function (data) {
          self.syncStatuses = data || []
          var anySyncing = data.some(function (s) { return s.syncing })
          if (!anySyncing) {
            clearInterval(poll)
            self.showToast('Sync complete', 'success')
          }
        })
      }, 2000)
    },

    // --- Toasts ---
    showToast: function (message, type) {
      var toast = { id: Date.now(), message: message, type: type || 'success' }
      this.toasts.push(toast)
      var self = this
      setTimeout(function () {
        self.toasts = self.toasts.filter(function (t) { return t.id !== toast.id })
      }, 4000)
    },

    // --- Helpers ---
    escapeHtml: function (s) {
      if (!s) return ''
      var div = document.createElement('div')
      div.textContent = s
      return div.innerHTML
    },

    formatSize: function (bytes) {
      if (!bytes) return ''
      if (bytes < 1024) return bytes + ' B'
      if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'
      return (bytes / 1048576).toFixed(1) + ' MB'
    },

    pagerRange: function () {
      var pages = []
      var total = this.totalPages
      var current = this.currentPage
      var start = Math.max(0, current - 2)
      var end = Math.min(total - 1, current + 2)

      if (start > 0) pages.push({ num: 0, label: '1' })
      if (start > 1) pages.push({ num: -1, label: '...' })

      for (var i = start; i <= end; i++) {
        pages.push({ num: i, label: String(i + 1) })
      }

      if (end < total - 2) pages.push({ num: -1, label: '...' })
      if (end < total - 1) pages.push({ num: total - 1, label: String(total) })

      return pages
    }
  },

  template: /* html */ '\
<div>\
  <!-- Header -->\
  <header class="app-header">\
    <a class="logo" href="#" @click.prevent="navigate(\'#\')">Mail Archive</a>\
    <nav class="header-nav">\
      <a href="#" :class="{active: view === \'search\'}" @click.prevent="navigate(\'#\')">Search</a>\
      <a href="#/accounts" :class="{active: view === \'accounts\'}" @click.prevent="navigate(\'#/accounts\')">Accounts</a>\
      <a href="#/sync" :class="{active: view === \'sync\'}" @click.prevent="navigate(\'#/sync\')">Sync</a>\
    </nav>\
    <div class="header-right" v-if="user">\
      <img v-if="user.avatar_url" :src="user.avatar_url" class="user-avatar" :alt="user.name">\
      <span class="user-name">{{ user.name }}</span>\
      <button class="btn btn-sm" @click="logout">Logout</button>\
    </div>\
  </header>\
\
  <!-- Search View -->\
  <div v-if="view === \'search\'" class="container">\
    <div class="search-box">\
      <svg class="search-icon" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><circle cx="11" cy="11" r="8"/><path stroke-linecap="round" d="m21 21-4.35-4.35"/></svg>\
      <input type="text" v-model="searchQuery" @input="onSearchInput" placeholder="Search emails by subject and body..." autofocus>\
    </div>\
    <div class="search-meta">\
      <span v-if="searchResults">\
        {{ searchResults.total || 0 }} result{{ searchResults.total !== 1 ? "s" : "" }}\
      </span>\
      <div style="display:flex;gap:0.5rem">\
        <button class="btn btn-sm" :class="{\'btn-primary\': searchMode === \'keyword\'}" @click="setSearchMode(\'keyword\')">Keyword</button>\
        <button class="btn btn-sm" :class="{\'btn-primary\': searchMode === \'similarity\'}" @click="setSearchMode(\'similarity\')">Similarity</button>\
      </div>\
    </div>\
    <div v-if="searchResults && searchResults.hits.length">\
      <a v-for="hit in searchResults.hits" :key="hit.path" class="email-card" :href="\'#/email/\' + encodeURIComponent(hit.path)">\
        <div class="email-subject" v-html="highlightText(hit.subject || \'(no subject)\', searchQuery)"></div>\
        <div v-if="hit.snippet" class="email-snippet" v-html="highlightText(hit.snippet, searchQuery)"></div>\
        <div class="email-meta-row">\
          <span>From: {{ hit.from }}</span>\
          <span>To: {{ hit.to }}</span>\
          <span>{{ formatDate(hit.date) }}</span>\
        </div>\
      </a>\
      <div v-if="totalPages > 1" class="pager">\
        <button :disabled="currentPage === 0" @click="goToPage(currentPage - 1)">&lsaquo; Prev</button>\
        <template v-for="p in pagerRange()" :key="p.label">\
          <button v-if="p.num >= 0" :class="{active: p.num === currentPage}" @click="goToPage(p.num)">{{ p.label }}</button>\
          <span v-else style="color:var(--text-dim);font-size:0.82rem">{{ p.label }}</span>\
        </template>\
        <button :disabled="currentPage >= totalPages - 1" @click="goToPage(currentPage + 1)">Next &rsaquo;</button>\
      </div>\
    </div>\
    <div v-else-if="searchResults" class="empty-state">\
      <p>No emails found{{ searchQuery ? " for \\"" + searchQuery + "\\"" : "" }}</p>\
    </div>\
  </div>\
\
  <!-- Accounts View -->\
  <div v-if="view === \'accounts\'" class="container">\
    <div class="page-title">\
      <span>Email Accounts</span>\
      <button class="btn btn-primary btn-sm" @click="openAddAccount">+ Add Account</button>\
    </div>\
    <div class="card">\
      <div v-if="accounts.length === 0" class="card-body">\
        <div class="empty-state"><p>No email accounts configured yet.</p></div>\
      </div>\
      <div v-for="acct in accounts" :key="acct.id" class="account-item">\
        <div class="account-icon">{{ accountIcon(acct.type) }}</div>\
        <div class="account-info">\
          <div class="account-email">{{ acct.email }}</div>\
          <div class="account-meta">\
            <span :class="accountTypeBadge(acct.type)">{{ acct.type }}</span>\
            <span v-if="acct.host">{{ acct.host }}:{{ acct.port }}</span>\
            <span>Every {{ acct.sync.interval }}</span>\
          </div>\
        </div>\
        <div class="account-actions">\
          <button class="btn btn-sm" @click="triggerSync(acct.id)">Sync</button>\
          <button class="btn btn-sm" @click="openEditAccount(acct)">Edit</button>\
          <button class="btn btn-sm btn-danger" @click="deleteAccount(acct)">Delete</button>\
        </div>\
      </div>\
    </div>\
  </div>\
\
  <!-- Sync View -->\
  <div v-if="view === \'sync\'" class="container">\
    <div class="page-title">\
      <span>Sync Status</span>\
      <button class="btn btn-primary btn-sm" @click="triggerSync(null)">Sync All</button>\
    </div>\
    <div class="card">\
      <div v-if="syncStatuses.length === 0" class="card-body">\
        <div class="empty-state"><p>No sync data available.</p></div>\
      </div>\
      <div v-for="s in syncStatuses" :key="s.id" class="account-item">\
        <div class="account-icon">{{ accountIcon(s.type) }}</div>\
        <div class="account-info">\
          <div class="account-email">{{ s.name }}</div>\
          <div class="account-meta">\
            <span :class="\'badge badge-\' + s.type.toLowerCase()">{{ s.type }}</span>\
          </div>\
        </div>\
        <div class="sync-status" :class="{running: s.syncing, done: !s.syncing && s.last_sync, error: s.last_error}">\
          <span v-if="s.syncing" class="spinner"></span>\
          <span v-if="s.syncing">Syncing...</span>\
          <span v-else-if="s.last_error">Error</span>\
          <span v-else-if="s.last_sync">{{ s.new_messages ? "+" + s.new_messages + " new" : "Up to date" }}</span>\
          <span v-else>Never synced</span>\
        </div>\
        <button class="btn btn-sm" @click="triggerSync(s.id)" :disabled="s.syncing">Sync</button>\
      </div>\
    </div>\
  </div>\
\
  <!-- Email Detail View -->\
  <div v-if="view === \'detail\'" class="container">\
    <a class="back-link" @click.prevent="goBack">\
      <svg width="16" height="16" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M15.75 19.5 8.25 12l7.5-7.5"/></svg>\
      Back to results\
    </a>\
    <div v-if="loading" style="text-align:center;padding:3rem"><span class="spinner" style="width:32px;height:32px;border-width:3px"></span></div>\
    <div v-else-if="selectedEmail" class="detail-card">\
      <div class="detail-header">\
        <div class="detail-subject">{{ selectedEmail.subject || "(no subject)" }}</div>\
        <dl class="detail-fields">\
          <dt>From</dt><dd>{{ selectedEmail.from }}</dd>\
          <dt>To</dt><dd>{{ selectedEmail.to }}</dd>\
          <template v-if="selectedEmail.cc"><dt>CC</dt><dd>{{ selectedEmail.cc }}</dd></template>\
          <dt>Date</dt><dd>{{ formatDate(selectedEmail.date) }}</dd>\
          <dt>Path</dt><dd style="font-size:0.8rem;color:var(--text-dim)">{{ selectedEmail.path }}</dd>\
        </dl>\
      </div>\
      <div v-if="selectedEmail.html_body" class="detail-body">\
        <iframe id="email-iframe" sandbox="allow-same-origin"></iframe>\
      </div>\
      <div v-else-if="selectedEmail.text_body" class="detail-body-text">{{ selectedEmail.text_body }}</div>\
      <div v-if="selectedEmail.attachments && selectedEmail.attachments.length" style="border-top:1px solid var(--border);padding:1rem 1.5rem">\
        <h3 style="font-size:0.82rem;color:var(--text-dim);font-weight:600;margin-bottom:0.5rem;text-transform:uppercase">Attachments ({{ selectedEmail.attachments.length }})</h3>\
        <div style="display:flex;flex-wrap:wrap;gap:0.5rem">\
          <span v-for="att in selectedEmail.attachments" :key="att.filename" style="background:var(--surface-2);border:1px solid var(--border);border-radius:6px;padding:0.4rem 0.75rem;font-size:0.8rem;display:flex;align-items:center;gap:0.4rem">\
            {{ att.filename || "unnamed" }} <span style="color:var(--text-dim)">{{ formatSize(att.size) }}</span>\
          </span>\
        </div>\
      </div>\
    </div>\
  </div>\
\
  <!-- Add/Edit Account Modal -->\
  <div v-if="showAddAccount" class="modal-backdrop" @click.self="showAddAccount = false">\
    <div class="modal">\
      <div class="modal-header">\
        <h2>{{ editingAccount ? "Edit Account" : "Add Email Account" }}</h2>\
        <button class="close-btn" @click="showAddAccount = false">&times;</button>\
      </div>\
      <div class="modal-body">\
        <div class="form-group">\
          <label>Account Type</label>\
          <select class="form-control" v-model="newAccount.type" @change="updatePortForType">\
            <option value="IMAP">IMAP</option>\
            <option value="POP3">POP3</option>\
            <option value="GMAIL_API">Gmail API</option>\
          </select>\
        </div>\
        <div class="form-group">\
          <label>Email Address</label>\
          <input class="form-control" v-model="newAccount.email" type="email" placeholder="user@example.com">\
        </div>\
        <template v-if="newAccount.type !== \'GMAIL_API\'">\
          <div class="form-row">\
            <div class="form-group">\
              <label>Host</label>\
              <input class="form-control" v-model="newAccount.host" placeholder="imap.gmail.com">\
            </div>\
            <div class="form-group">\
              <label>Port</label>\
              <input class="form-control" v-model.number="newAccount.port" type="number">\
            </div>\
          </div>\
          <div class="form-group">\
            <label>Password / App Password</label>\
            <input class="form-control" v-model="newAccount.password" type="password">\
          </div>\
          <div class="form-row">\
            <div class="form-group">\
              <label>Folders</label>\
              <input class="form-control" v-model="newAccount.folders" placeholder="all">\
            </div>\
            <div class="form-group">\
              <label>Sync Interval</label>\
              <input class="form-control" v-model="newAccount.sync.interval" placeholder="5m">\
            </div>\
          </div>\
        </template>\
      </div>\
      <div class="modal-footer">\
        <button class="btn btn-sm" @click="showAddAccount = false">Cancel</button>\
        <button class="btn btn-primary btn-sm" @click="saveAccount">{{ editingAccount ? "Update" : "Add Account" }}</button>\
      </div>\
    </div>\
  </div>\
\
  <!-- Toasts -->\
  <div class="toast-container">\
    <div v-for="toast in toasts" :key="toast.id" class="toast" :class="toast.type">\
      {{ toast.message }}\
    </div>\
  </div>\
</div>'
}

// Mount Vue app.
var app = Vue.createApp(App)
app.mount('#app')
