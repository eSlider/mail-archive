<div class="app-root" :class="{ 'view-detail': view === 'detail' }">
  <!-- Header -->
  <header class="app-header">
    <a class="logo" href="#" @click.prevent="navigate('#')">Mail Archive</a>
    <nav class="header-nav header-nav-desktop">
      <a href="#" :class="{active: view === 'search'}" @click.prevent="navigate('#')">Search</a>
      <a href="#/accounts" :class="{active: view === 'accounts'}" @click.prevent="navigate('#/accounts')">Accounts</a>
      <a href="#/import" :class="{active: view === 'import'}" @click.prevent="navigate('#/import')">Import</a>
    </nav>
    <div class="header-right" v-if="user">
      <a href="https://github.com/eSlider/mail-archive" target="_blank" rel="noopener" class="github-link" title="View on GitHub">
        <svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.65 7.65 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
      </a>
      <img v-if="user.avatar_url" :src="user.avatar_url" class="user-avatar" :alt="user.name">
      <span class="user-name">{{ user.name }}</span>
      <button class="btn btn-sm" @click="logout">Logout</button>
    </div>
  </header>

  <!-- Search View -->
  <div v-if="view === 'search'" class="container">
    <div class="search-box">
      <svg class="search-icon" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><circle cx="11" cy="11" r="8"/><path stroke-linecap="round" d="m21 21-4.35-4.35"/></svg>
      <input type="text" v-model="searchQuery" @input="onSearchInput" placeholder="Search emails by subject and body..." autofocus>
    </div>
    <div class="search-meta">
      <span v-if="searchResults">
        {{ searchResults.total || 0 }} result{{ searchResults.total !== 1 ? "s" : "" }}
      </span>
      <div style="display:flex;flex-wrap:wrap;gap:0.5rem;align-items:center">
        <div v-if="accounts.length" class="search-account-mask" title="Toggle accounts included in search">
          <span style="font-size:0.8rem;color:var(--text-dim);margin-right:0.25rem">Accounts:</span>
          <label v-for="acct in accounts" :key="acct.id" class="search-account-check">
            <input type="checkbox" :checked="isSearchAccountEnabled(acct.id)" @change="toggleSearchAccount(acct.id)">
            <span>{{ acct.email }}</span>
          </label>
        </div>
        <button class="btn btn-sm" :class="{'btn-primary': searchMode === 'keyword'}" @click="setSearchMode('keyword')">Keyword</button>
        <button class="btn btn-sm" :class="{'btn-primary': searchMode === 'similarity'}" @click="setSearchMode('similarity')">Similarity</button>
      </div>
    </div>
    <div v-if="searchResults && searchResults.hits.length" class="search-results-wrap">
      <div v-if="searchResults.total > 0" class="search-progress-wrap">
        <div class="search-progress-bar" :title="searchResults.hits.length + ' / ' + searchResults.total + ' (page ' + (currentPage + 1) + ' of ' + totalPages + ')'">
          <div class="search-progress-fill" :style="{ height: searchLoadProgress + '%' }"></div>
          <span class="search-progress-label">{{ searchResults.hits.length }} / {{ searchResults.total }}</span>
        </div>
      </div>
      <a v-for="hit in searchResults.hits" :key="(hit.account_id || '') + '/' + hit.path" class="email-card" :href="emailDetailHref(hit)">
        <div class="email-subject-row">
          <span class="email-subject" v-html="highlightText(hit.subject || '(no subject)', searchQuery)"></span>
          <span v-if="folderFromPath(hit.path)" class="email-folder">{{ folderFromPath(hit.path) }}</span>
        </div>
        <div v-if="hit.snippet" class="email-snippet" v-html="highlightText(hit.snippet, searchQuery)"></div>
        <div class="email-meta-row">
          <span>From: {{ hit.from }}</span>
          <span>To: {{ hit.to }}</span>
          <span>{{ formatDate(hit.date) }}</span>
        </div>
      </a>
      <!-- Infinite scroll sentinel + Load more button -->
      <div v-if="totalPages > 1 && currentPage < totalPages - 1" ref="loadMoreSentinel" class="load-more-sentinel">
        <button class="btn btn-sm load-more-btn" @click="loadMore" :disabled="loadingMore">
          <span v-if="loadingMore" class="spinner spinner-sm"></span>
          <span>{{ loadingMore ? 'Loading...' : 'Load more' }}</span>
        </button>
      </div>
    </div>
    <div v-else-if="searchResults" class="empty-state">
      <p>No emails found{{ searchQuery ? ' for "' + searchQuery + '"' : "" }}</p>
    </div>
  </div>

  <!-- Accounts View (merged with Sync) -->
  <div v-if="view === 'accounts'" class="container">
    <div class="page-title">
      <span>Email Accounts &amp; Sync</span>
      <button class="btn btn-primary btn-sm" @click="triggerSync(null)">Sync All</button>
      <button class="btn btn-sm" @click="openAddAccount">+ Add Account</button>
    </div>
    <div class="card">
      <div v-if="accounts.length === 0" class="card-body">
        <div class="empty-state"><p>No email accounts configured yet.</p></div>
      </div>
      <div v-for="acct in accounts" :key="acct.id" class="account-item" :class="{syncing: acct.type !== 'PST' && accountSyncStatus(acct.id).syncing}">
        <div class="account-icon">{{ accountIcon(acct.type) }}</div>
        <div class="account-info">
          <div class="account-email">
            {{ acct.email }}
            <span v-if="acct.type !== 'PST' && accountSyncStatus(acct.id).syncing" class="badge badge-syncing">
              <span class="spinner spinner-sm"></span> syncing
            </span>
            <span v-if="acct.type !== 'PST' && accountSyncStatus(acct.id).last_error && !accountSyncStatus(acct.id).syncing" class="badge badge-error">error</span>
          </div>
          <div class="account-meta">
            <span :class="accountTypeBadge(acct.type)">{{ acct.type }}</span>
            <span v-if="acct.type === 'PST'">Import only</span>
            <span v-else>
              <span v-if="acct.host">{{ acct.host }}:{{ acct.port }}</span>
              <span>Every {{ acct.sync.interval }}</span>
              <span v-if="accountSyncStatus(acct.id).last_sync" class="sync-status-inline" :class="{done: !accountSyncStatus(acct.id).syncing}">
                · {{ accountSyncStatus(acct.id).syncing ? 'Syncing...' : (accountSyncStatus(acct.id).new_messages ? '+' + accountSyncStatus(acct.id).new_messages + ' new' : 'Up to date') }}
              </span>
            </span>
          </div>
          <div v-if="acct.type !== 'PST' && accountSyncStatus(acct.id).progress && accountSyncStatus(acct.id).syncing" class="account-progress">{{ accountSyncStatus(acct.id).progress }}</div>
          <div v-if="acct.type !== 'PST' && accountSyncStatus(acct.id).last_error && !accountSyncStatus(acct.id).syncing" class="account-error">{{ accountSyncStatus(acct.id).last_error }}</div>
        </div>
        <div class="account-actions">
          <template v-if="acct.type === 'PST'">
            <a href="#/import" class="btn btn-sm" @click.prevent="navigate('#/import')">Import</a>
          </template>
          <template v-else>
            <button v-if="accountSyncStatus(acct.id).syncing" class="btn btn-sm btn-danger" @click="stopSync(acct.id)">Stop</button>
            <button v-else class="btn btn-sm" @click="triggerSync(acct.id)">Sync</button>
          </template>
          <button class="btn btn-sm" @click="openEditAccount(acct)">Edit</button>
          <button class="btn btn-sm btn-danger" @click="deleteAccount(acct)">Delete</button>
        </div>
      </div>
    </div>
  </div>

  <!-- Import PST/OST View -->
  <div v-if="view === 'import'" class="container">
    <div class="page-title">
      <span>Import PST / OST</span>
    </div>
    <div class="card">
      <div class="card-body">
        <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:1.5rem">
          Upload a Microsoft Outlook PST or OST file to import emails. Files up to 10GB+ are supported via streaming upload.
        </p>
        <div class="form-group">
          <label>Title (account name)</label>
          <input class="form-control" v-model="importTitle" placeholder="My Outlook Archive">
        </div>
        <div class="form-group">
          <label>Select PST/OST file</label>
          <input type="file" ref="pstFile" class="form-control" accept=".pst,.ost" @change="onPSTFileSelected">
        </div>
        <button class="btn btn-primary" @click="startPSTImport" :disabled="importRunning || !importFile">
          {{ importRunning ? 'Importing...' : 'Upload & Import' }}
        </button>
      </div>

      <!-- Import Progress -->
      <div v-if="importJob" class="import-progress">
        <div class="progress-header">
          <span class="progress-phase" :class="importJob.phase">{{ importPhaseLabel }}</span>
          <span class="progress-detail">{{ importProgressDetail }}</span>
        </div>
        <div class="progress-bar-wrapper">
          <div class="progress-bar" :style="{width: importPercent + '%'}"></div>
        </div>
        <div v-if="importJob.error" class="account-error" style="margin-top:0.75rem">{{ importJob.error }}</div>
      </div>

      <!-- Past imports -->
      <div v-if="importHistory.length" style="border-top:1px solid var(--border);padding:1rem 1.5rem">
        <h3 style="font-size:0.82rem;color:var(--text-dim);font-weight:600;margin-bottom:0.5rem;text-transform:uppercase">Recent Imports</h3>
        <div v-for="h in importHistory" :key="h.id" class="account-item" style="padding:0.5rem 0">
          <div class="account-icon">&#x1F4C2;</div>
          <div class="account-info">
            <div class="account-email">{{ h.filename }}</div>
            <div class="account-meta">
              <span class="badge" :class="'badge-' + h.phase">{{ h.phase }}</span>
              <span v-if="h.current">{{ h.current }} messages</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- Email Detail View -->
  <div v-if="view === 'detail'" class="container detail-container detail-swipe-area"
    @touchstart="onDetailTouchStart"
    @touchend="onDetailTouchEnd">
    <div class="detail-nav-bar">
      <div class="detail-nav-group">
        <button v-if="detailPrevHit" class="btn-icon" @click="goToEmail(detailPrevHit)" title="Previous email" aria-label="Previous email">
          <svg width="20" height="20" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M15.75 19.5 8.25 12l7.5-7.5"/></svg>
        </button>
        <button class="btn-icon btn-back" @click="goBack" title="Back to results" aria-label="Back to results">
          <svg width="20" height="20" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M9 15 3 9m0 0 6-6M3 9h12a6 6 0 0 1 0 12h-3"/></svg>
        </button>
        <button v-if="detailNextHit" class="btn-icon" @click="goToEmail(detailNextHit)" title="Next" aria-label="Next email">
          <svg width="20" height="20" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M8.25 4.5 15.75 12l-7.5 7.5"/></svg>
        </button>
        <span v-if="detailCountDisplay" class="detail-count">{{ detailCountDisplay }}</span>
      </div>
      <a v-if="selectedEmail" :href="emailDownloadUrl()" class="btn btn-sm btn-detail-download" download>
        <svg width="14" height="14" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/></svg>
        Download
      </a>
    </div>
    <div v-if="loading" style="text-align:center;padding:3rem"><span class="spinner" style="width:32px;height:32px;border-width:3px"></span></div>
    <div v-else-if="selectedEmail" class="detail-card">
      <div class="detail-header">
        <div class="detail-subject">{{ selectedEmail.subject || "(no subject)" }}</div>
        <dl class="detail-fields">
          <dt>From</dt><dd>{{ selectedEmail.from }}</dd>
          <dt>To</dt><dd>{{ selectedEmail.to }}</dd>
          <template v-if="selectedEmail.cc"><dt>CC</dt><dd>{{ selectedEmail.cc }}</dd></template>
          <dt>Date</dt><dd>{{ formatDate(selectedEmail.date) }}</dd>
          <dt>Path</dt><dd style="font-size:0.8rem;color:var(--text-dim)">{{ selectedEmail.path }}</dd>
        </dl>
      </div>
      <div v-if="selectedEmail.html_body" class="detail-body">
        <iframe id="email-iframe" sandbox="allow-same-origin"></iframe>
      </div>
      <div v-else-if="selectedEmail.text_body" class="detail-body-text">{{ selectedEmail.text_body }}</div>
      <div v-if="selectedEmail.attachments && selectedEmail.attachments.length" style="border-top:1px solid var(--border);padding:1rem 1.5rem">
        <h3 style="font-size:0.82rem;color:var(--text-dim);font-weight:600;margin-bottom:0.5rem;text-transform:uppercase">Attachments ({{ selectedEmail.attachments.length }})</h3>
        <div style="display:flex;flex-wrap:wrap;gap:0.5rem">
          <a v-for="(att, idx) in selectedEmail.attachments" :key="att.filename + '-' + idx" :href="attachmentDownloadUrl(idx)" :download="att.filename || 'attachment'" style="background:var(--surface-2);border:1px solid var(--border);border-radius:6px;padding:0.4rem 0.75rem;font-size:0.8rem;display:flex;align-items:center;gap:0.4rem;text-decoration:none;color:inherit;cursor:pointer;transition:border-color 0.15s"
            @mouseover="$event.currentTarget.style.borderColor='var(--primary)'" @mouseout="$event.currentTarget.style.borderColor='var(--border)'">
            <svg width="12" height="12" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/></svg>
            {{ att.filename || "unnamed" }} <span style="color:var(--text-dim)">{{ formatSize(att.size) }}</span>
          </a>
        </div>
      </div>
    </div>
  </div>

  <!-- Add/Edit Account Modal -->
  <div v-if="showAddAccount" class="modal-backdrop" @click.self="showAddAccount = false">
    <div class="modal">
      <div class="modal-header">
        <h2>{{ editingAccount ? "Edit Account" : "Add Email Account" }}</h2>
        <button class="close-btn" @click="showAddAccount = false">&times;</button>
      </div>
      <div class="modal-body">
        <div class="form-group">
          <label>Account Type</label>
          <select class="form-control" v-model="newAccount.type" @change="updatePortForType">
            <option value="IMAP">IMAP</option>
            <option value="POP3">POP3</option>
            <option value="GMAIL_API">Gmail API</option>
          </select>
        </div>
        <div class="form-group">
          <label>Email Address</label>
          <input class="form-control" v-model="newAccount.email" type="email" placeholder="user@example.com">
        </div>
        <template v-if="newAccount.type !== 'GMAIL_API'">
          <div class="form-row">
            <div class="form-group">
              <label>Host</label>
              <input class="form-control" v-model="newAccount.host" placeholder="imap.gmail.com">
            </div>
            <div class="form-group">
              <label>Port</label>
              <input class="form-control" v-model.number="newAccount.port" type="number">
            </div>
          </div>
          <div class="form-group">
            <label>Password / App Password</label>
            <input class="form-control" v-model="newAccount.password" type="password">
          </div>
          <div class="form-row">
            <div class="form-group">
              <label>Folders</label>
              <input class="form-control" v-model="newAccount.folders" placeholder="all">
            </div>
            <div class="form-group">
              <label>Sync Interval</label>
              <input class="form-control" v-model="newAccount.sync.interval" placeholder="5m">
            </div>
          </div>
        </template>
      </div>
      <div class="modal-footer">
        <button class="btn btn-sm" @click="showAddAccount = false">Cancel</button>
        <button class="btn btn-primary btn-sm" @click="saveAccount">{{ editingAccount ? "Update" : "Add Account" }}</button>
      </div>
    </div>
  </div>

  <!-- Mobile bottom navigation -->
  <nav class="bottom-nav" v-if="user">
    <a href="#" class="bottom-nav-item" :class="{active: view === 'search'}" @click.prevent="navigate('#')">
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><circle cx="11" cy="11" r="8"/><path stroke-linecap="round" d="m21 21-4.35-4.35"/></svg>
      <span>Search</span>
    </a>
    <a href="#/accounts" class="bottom-nav-item" :class="{active: view === 'accounts'}" @click.prevent="navigate('#/accounts')">
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M21.75 6.75v10.5a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25m19.5 0v.243a2.25 2.25 0 0 1-1.07 1.916l-7.5 4.615a2.25 2.25 0 0 1-2.36 0L3.32 8.91a2.25 2.25 0 0 1-1.07-1.916V6.75"/></svg>
      <span>Accounts</span>
    </a>
    <a href="#/import" class="bottom-nav-item" :class="{active: view === 'import'}" @click.prevent="navigate('#/import')">
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5m-13.5-9L12 3m0 0 4.5 4.5M12 3v13.5"/></svg>
      <span>Import</span>
    </a>
  </nav>

  <!-- Toasts -->
  <div class="toast-container">
    <div v-for="toast in toasts" :key="toast.id" class="toast" :class="toast.type">
      {{ toast.message }}
    </div>
  </div>
</div>
