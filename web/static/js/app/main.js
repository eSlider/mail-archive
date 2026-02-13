// Mail Archive — Main Application (Vue 3 + jQuery)
// No TypeScript, no CDN — all resources are local.
// Template is loaded from main.template.vue at startup (fetched as raw HTML).

(async function () {
  'use strict';

  // Fetch Vue template before creating the app.
  var templateResponse = await fetch('/static/js/app/main.template.vue');
  var templateHTML = await templateResponse.text();

  var App = {
    template: templateHTML,

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
        var icons = { IMAP: '\u{1F4E8}', POP3: '\u{1F4EC}', GMAIL_API: '\u{1F4E7}' }
        return icons[type] || '\u{1F4E7}'
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
    }
  }

  // Mount Vue app.
  var app = Vue.createApp(App)
  app.mount('#app')
})();
