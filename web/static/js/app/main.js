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
        searchAccountMask: {},
        currentPage: 0,
        pageSize: 50,
        selectedEmail: null,
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
      }
    },

    computed: {
      totalPages: function () {
        if (!this.searchResults) return 0
        return Math.ceil(this.searchResults.total / this.pageSize)
      },

      importPhaseLabel: function () {
        if (!this.importJob) return ''
        var labels = {
          uploading: 'Uploading...',
          extracting: 'Extracting messages...',
          indexing: 'Building search index...',
          done: 'Import complete',
          error: 'Import failed'
        }
        return labels[this.importJob.phase] || this.importJob.phase
      },

      importProgressDetail: function () {
        if (!this.importJob) return ''
        var j = this.importJob
        if (j.phase === 'uploading' && j.total > 0) {
          return j.current + ' / ' + j.total + ' MB'
        }
        if (j.phase === 'extracting') {
          return j.current + ' messages'
        }
        if (j.phase === 'done') {
          return j.current + ' messages imported'
        }
        return ''
      },

      importPercent: function () {
        if (!this.importJob) return 0
        var j = this.importJob
        if (j.phase === 'done') return 100
        if (j.phase === 'error') return 0
        if (j.total > 0) return Math.min(99, Math.round(j.current / j.total * 100))
        if (j.phase === 'extracting' && j.current > 0) return 50
        if (j.phase === 'indexing') return 90
        return 0
      }
    },

    created: function () {
      this.loadUser()
      this.loadAccounts()
      this.doSearch('', 0)
      this.startSyncPoll()

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
          var rest = hash.slice(8)
          var qIdx = rest.indexOf('?')
          var emailPath = decodeURIComponent(qIdx >= 0 ? rest.slice(0, qIdx) : rest)
          var accountId = null
          if (qIdx >= 0) {
            var params = new URLSearchParams(rest.slice(qIdx))
            accountId = params.get('account_id')
          }
          this.showEmailDetail(emailPath, accountId)
        } else if (hash === '#/accounts') {
          this.view = 'accounts'
          this.loadSyncStatus()
        } else if (hash === '#/import') {
          this.view = 'import'
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
        var icons = { IMAP: '\u{1F4E8}', POP3: '\u{1F4EC}', GMAIL_API: '\u{1F4E7}', PST: '\u{1F4C2}' }
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
        var ids = this.enabledSearchAccountIds()
        if (ids.length > 0 && ids.length < this.accounts.length) {
          url += '&account_ids=' + encodeURIComponent(ids.join(','))
        }
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

      isSearchAccountEnabled: function (acctId) {
        return this.searchAccountMask[acctId] !== false
      },

      toggleSearchAccount: function (acctId) {
        this.searchAccountMask = Object.assign({}, this.searchAccountMask, { [acctId]: !this.isSearchAccountEnabled(acctId) })
        this.currentPage = 0
        this.doSearch(this.searchQuery, 0)
      },

      enabledSearchAccountIds: function () {
        var self = this
        return this.accounts.filter(function (a) { return self.isSearchAccountEnabled(a.id) }).map(function (a) { return a.id })
      },

      emailDetailHref: function (hit) {
        var h = '#/email/' + encodeURIComponent(hit.path)
        if (hit.account_id) h += '?account_id=' + encodeURIComponent(hit.account_id)
        return h
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
      showEmailDetail: function (path, accountId) {
        var self = this
        this.view = 'detail'
        this.loading = true
        this.selectedEmail = null

        var url = '/api/email?path=' + encodeURIComponent(path)
        if (accountId) url += '&account_id=' + encodeURIComponent(accountId)
        $.getJSON(url).done(function (data) {
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

      stopSync: function (accountID) {
        var self = this
        $.ajax({
          url: '/api/sync/stop',
          type: 'POST',
          contentType: 'application/json',
          data: JSON.stringify({ account_id: accountID })
        }).done(function () {
          self.showToast('Sync stopped', 'warning')
          self.refreshSyncStatus()
        }).fail(function () {
          self.showToast('Failed to stop sync', 'error')
        })
      },

      accountSyncStatus: function (accountID) {
        return this.syncStatusMap[accountID] || {}
      },

      startSyncPoll: function () {
        var self = this
        this.refreshSyncStatus()
        this.accountPollTimer = setInterval(function () {
          self.refreshSyncStatus()
        }, 3000)
      },

      refreshSyncStatus: function () {
        var self = this
        $.getJSON('/api/sync/status').done(function (data) {
          self.syncStatuses = data || []
          var m = {}
          for (var i = 0; i < (data || []).length; i++) {
            m[data[i].id] = data[i]
          }
          self.syncStatusMap = m

          // Show error notifications for accounts with errors.
          for (var j = 0; j < (data || []).length; j++) {
            var s = data[j]
            if (s.last_error && !s.syncing) {
              var key = 'sync_error_' + s.id
              if (!self._shownErrors) self._shownErrors = {}
              if (!self._shownErrors[key]) {
                self._shownErrors[key] = true
                self.showToast(s.name + ': ' + s.last_error, 'error')
              }
            } else {
              if (self._shownErrors) delete self._shownErrors['sync_error_' + s.id]
            }
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

      // --- PST/OST Import ---
      onPSTFileSelected: function (e) {
        this.importFile = e.target.files[0] || null
        if (this.importFile && !this.importTitle) {
          this.importTitle = this.importFile.name.replace(/\.(pst|ost)$/i, '')
        }
      },

      startPSTImport: function () {
        if (!this.importFile) return
        var self = this
        this.importRunning = true
        this.importJob = { phase: 'uploading', current: 0, total: 0 }

        var formData = new FormData()
        formData.append('file', this.importFile)
        formData.append('title', this.importTitle || this.importFile.name)

        $.ajax({
          url: '/api/import/pst',
          type: 'POST',
          data: formData,
          processData: false,
          contentType: false,
          xhr: function () {
            var xhr = new XMLHttpRequest()
            xhr.upload.addEventListener('progress', function (e) {
              if (e.lengthComputable) {
                self.importJob = {
                  phase: 'uploading',
                  current: Math.round(e.loaded / (1024 * 1024)),
                  total: Math.round(e.total / (1024 * 1024))
                }
              }
            })
            return xhr
          }
        }).done(function (data) {
          self.importJob = { id: data.job_id, phase: 'extracting', current: 0, total: 0 }
          self.pollImportStatus(data.job_id)
          self.showToast('Upload complete, extracting messages...', 'success')
        }).fail(function (xhr) {
          self.importRunning = false
          var msg = 'Upload failed'
          try { msg = JSON.parse(xhr.responseText).error } catch (e) {}
          self.importJob = { phase: 'error', error: msg }
          self.showToast(msg, 'error')
        })
      },

      pollImportStatus: function (jobID) {
        var self = this
        if (this.importPollTimer) clearInterval(this.importPollTimer)
        this.importPollTimer = setInterval(function () {
          $.getJSON('/api/import/status/' + jobID).done(function (data) {
            self.importJob = data
            if (data.phase === 'done') {
              clearInterval(self.importPollTimer)
              self.importPollTimer = null
              self.importRunning = false
              self.importHistory.unshift(data)
              self.loadAccounts()
              self.showToast('Import complete: ' + data.current + ' messages', 'success')
            } else if (data.phase === 'error') {
              clearInterval(self.importPollTimer)
              self.importPollTimer = null
              self.importRunning = false
              self.importHistory.unshift(data)
              self.showToast('Import failed: ' + data.error, 'error')
            }
          })
        }, 1500)
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
