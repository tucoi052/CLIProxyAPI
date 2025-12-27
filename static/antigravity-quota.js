/**
 * Antigravity Quota Display Module
 *
 * This is a standalone JavaScript module that can be injected into the CLIProxyAPI
 * management UI to add Antigravity quota display functionality.
 *
 * Features:
 * - Fetches quota data from /v0/management/antigravity-quota endpoint
 * - Displays quota as progress bars with percentages
 * - Auto-refreshes every 5 minutes
 * - Fully self-contained CSS and HTML
 *
 * Usage:
 * 1. Include this script in management.html or serve it separately
 * 2. Call AntigravityQuotaModule.init() when DOM is ready
 * 3. The module will automatically create a quota section in the UI
 */

(function(window) {
    'use strict';

    const AntigravityQuotaModule = {
        // Configuration
        config: {
            refreshInterval: 5 * 60 * 1000, // 5 minutes
            containerId: 'antigravity-quota-container',
            apiEndpoint: '/v0/management/antigravity-quota'
        },

        // State
        state: {
            isInitialized: false,
            refreshTimer: null,
            lastFetchTime: null
        },

        /**
         * Initialize the module
         */
        init: function() {
            if (this.state.isInitialized) {
                console.log('AntigravityQuotaModule already initialized');
                return;
            }

            console.log('Initializing AntigravityQuotaModule...');

            // Inject CSS
            this.injectStyles();

            // Create UI container
            this.createContainer();

            // Fetch initial data
            this.fetchQuota();

            // Setup auto-refresh
            this.setupAutoRefresh();

            this.state.isInitialized = true;
            console.log('AntigravityQuotaModule initialized successfully');
        },

        /**
         * Inject CSS styles into the page
         */
        injectStyles: function() {
            const styleId = 'antigravity-quota-styles';
            if (document.getElementById(styleId)) {
                return; // Already injected
            }

            const style = document.createElement('style');
            style.id = styleId;
            style.textContent = `
                .agq-container {
                    margin: 20px 0;
                    padding: 20px;
                    background: white;
                    border-radius: 8px;
                    box-shadow: 0 2px 4px rgba(0,0,0,0.1);
                }

                .agq-header {
                    display: flex;
                    justify-content: space-between;
                    align-items: center;
                    margin-bottom: 20px;
                    padding-bottom: 15px;
                    border-bottom: 2px solid #e2e8f0;
                }

                .agq-title {
                    font-size: 20px;
                    font-weight: 600;
                    color: #2d3748;
                    display: flex;
                    align-items: center;
                    gap: 10px;
                }

                .agq-refresh-btn {
                    padding: 8px 16px;
                    background: #667eea;
                    color: white;
                    border: none;
                    border-radius: 6px;
                    cursor: pointer;
                    font-size: 14px;
                    transition: all 0.2s;
                }

                .agq-refresh-btn:hover {
                    background: #5568d3;
                    transform: translateY(-1px);
                }

                .agq-refresh-btn:disabled {
                    background: #cbd5e0;
                    cursor: not-allowed;
                    transform: none;
                }

                .agq-stats {
                    display: grid;
                    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
                    gap: 15px;
                    margin-bottom: 20px;
                }

                .agq-stat {
                    padding: 15px;
                    background: #f7fafc;
                    border-radius: 6px;
                    border-left: 4px solid #667eea;
                }

                .agq-stat-label {
                    font-size: 12px;
                    color: #718096;
                    text-transform: uppercase;
                    margin-bottom: 5px;
                }

                .agq-stat-value {
                    font-size: 24px;
                    font-weight: bold;
                    color: #2d3748;
                }

                .agq-accounts {
                    display: grid;
                    gap: 15px;
                }

                .agq-account {
                    padding: 15px;
                    background: #f7fafc;
                    border-radius: 6px;
                    border: 1px solid #e2e8f0;
                }

                .agq-account-header {
                    display: flex;
                    justify-content: space-between;
                    align-items: center;
                    margin-bottom: 15px;
                }

                .agq-account-email {
                    font-weight: 600;
                    color: #2d3748;
                }

                .agq-status-badge {
                    padding: 4px 10px;
                    border-radius: 12px;
                    font-size: 11px;
                    font-weight: 600;
                    text-transform: uppercase;
                }

                .agq-status-active {
                    background: #c6f6d5;
                    color: #22543d;
                }

                .agq-status-error {
                    background: #fed7d7;
                    color: #742a2a;
                }

                .agq-status-forbidden {
                    background: #feebc8;
                    color: #7c2d12;
                }

                .agq-quota-item {
                    margin-bottom: 12px;
                }

                .agq-quota-header {
                    display: flex;
                    justify-content: space-between;
                    margin-bottom: 6px;
                }

                .agq-model-name {
                    font-size: 14px;
                    color: #4a5568;
                }

                .agq-quota-percent {
                    font-weight: 600;
                    color: #667eea;
                }

                .agq-quota-bar {
                    height: 6px;
                    background: #e2e8f0;
                    border-radius: 3px;
                    overflow: hidden;
                }

                .agq-quota-fill {
                    height: 100%;
                    background: linear-gradient(90deg, #667eea 0%, #764ba2 100%);
                    transition: width 0.3s;
                }

                .agq-error {
                    padding: 12px;
                    background: #fed7d7;
                    color: #742a2a;
                    border-radius: 6px;
                    margin-top: 10px;
                    font-size: 14px;
                }

                .agq-loading {
                    text-align: center;
                    padding: 30px;
                    color: #718096;
                }

                .agq-empty {
                    text-align: center;
                    padding: 40px;
                    color: #a0aec0;
                }

                .agq-last-update {
                    font-size: 12px;
                    color: #a0aec0;
                    margin-top: 10px;
                    text-align: right;
                }
            `;
            document.head.appendChild(style);
        },

        /**
         * Create the main container and inject it into the page
         */
        createContainer: function() {
            // Try to find a good place to inject the quota display
            // Look for existing management sections
            let targetElement = document.querySelector('.main-content') ||
                               document.querySelector('.content') ||
                               document.querySelector('main') ||
                               document.body;

            // Create container
            const container = document.createElement('div');
            container.id = this.config.containerId;
            container.className = 'agq-container';
            container.innerHTML = `
                <div class="agq-header">
                    <div class="agq-title">
                        <span>🚀</span>
                        <span>Antigravity Quota</span>
                    </div>
                    <button class="agq-refresh-btn" onclick="AntigravityQuotaModule.fetchQuota()">
                        🔄 Refresh
                    </button>
                </div>
                <div id="agq-content">
                    <div class="agq-loading">Loading quota data...</div>
                </div>
            `;

            // Insert at the beginning of the target element
            if (targetElement.firstChild) {
                targetElement.insertBefore(container, targetElement.firstChild);
            } else {
                targetElement.appendChild(container);
            }
        },

        /**
         * Fetch quota data from the API
         */
        fetchQuota: async function() {
            const contentEl = document.getElementById('agq-content');
            if (!contentEl) return;

            try {
                // Disable refresh button
                const refreshBtn = document.querySelector('.agq-refresh-btn');
                if (refreshBtn) refreshBtn.disabled = true;

                const response = await fetch(this.config.apiEndpoint, {
                    headers: {
                        'X-Management-Key': this.getManagementKey()
                    }
                });

                if (!response.ok) {
                    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
                }

                const data = await response.json();
                this.renderQuota(data);
                this.state.lastFetchTime = new Date();

            } catch (error) {
                console.error('Failed to fetch quota:', error);
                contentEl.innerHTML = `
                    <div class="agq-error">
                        ❌ Failed to load quota data: ${error.message}
                    </div>
                `;
            } finally {
                // Re-enable refresh button
                const refreshBtn = document.querySelector('.agq-refresh-btn');
                if (refreshBtn) refreshBtn.disabled = false;
            }
        },

        /**
         * Render quota data in the UI
         */
        renderQuota: function(data) {
            const contentEl = document.getElementById('agq-content');
            if (!contentEl) return;

            // Check if there are any accounts
            if (!data.accounts || data.accounts.length === 0) {
                contentEl.innerHTML = `
                    <div class="agq-empty">
                        <p>📭 No Antigravity accounts found</p>
                        <p style="font-size: 14px; margin-top: 10px;">Please log in to an Antigravity account first</p>
                    </div>
                `;
                return;
            }

            // Build HTML
            let html = `
                <div class="agq-stats">
                    <div class="agq-stat">
                        <div class="agq-stat-label">Total</div>
                        <div class="agq-stat-value">${data.total_accounts || 0}</div>
                    </div>
                    <div class="agq-stat">
                        <div class="agq-stat-label">Active</div>
                        <div class="agq-stat-value" style="color: #38a169;">${data.active_accounts || 0}</div>
                    </div>
                    <div class="agq-stat">
                        <div class="agq-stat-label">Inactive</div>
                        <div class="agq-stat-value" style="color: #dd6b20;">${data.inactive_accounts || 0}</div>
                    </div>
                    <div class="agq-stat">
                        <div class="agq-stat-label">Errors</div>
                        <div class="agq-stat-value" style="color: #e53e3e;">${data.error_accounts || 0}</div>
                    </div>
                </div>
                <div class="agq-accounts">
            `;

            // Render each account
            data.accounts.forEach(account => {
                const statusClass = `agq-status-${account.status}`;

                html += `
                    <div class="agq-account">
                        <div class="agq-account-header">
                            <div class="agq-account-email">${account.email || 'Unknown'}</div>
                            <span class="agq-status-badge ${statusClass}">${account.status}</span>
                        </div>
                `;

                if (account.project_id) {
                    html += `<div style="font-size: 12px; color: #718096; margin-bottom: 10px;">Project: ${account.project_id}</div>`;
                }

                if (account.model_quotas && account.model_quotas.length > 0) {
                    account.model_quotas.forEach(quota => {
                        const percent = quota.remaining_percent.toFixed(1);
                        html += `
                            <div class="agq-quota-item">
                                <div class="agq-quota-header">
                                    <span class="agq-model-name">${quota.display_name}</span>
                                    <span class="agq-quota-percent">${percent}%</span>
                                </div>
                                <div class="agq-quota-bar">
                                    <div class="agq-quota-fill" style="width: ${percent}%"></div>
                                </div>
                            </div>
                        `;
                    });
                } else {
                    html += '<p style="color: #a0aec0; font-size: 14px;">No quota information available</p>';
                }

                if (account.error) {
                    html += `<div class="agq-error">${account.error}</div>`;
                }

                html += '</div>';
            });

            html += `</div>`;

            if (this.state.lastFetchTime) {
                html += `<div class="agq-last-update">Last updated: ${this.state.lastFetchTime.toLocaleString()}</div>`;
            }

            contentEl.innerHTML = html;
        },

        /**
         * Get management API key from the page
         * This tries to find the key from common places in the management UI
         */
        getManagementKey: function() {
            // Try to get from localStorage (if management UI stores it there)
            let key = localStorage.getItem('managementKey') ||
                     localStorage.getItem('apiKey');

            // Try to get from a global variable (if management UI exposes it)
            if (!key && window.managementConfig) {
                key = window.managementConfig.apiKey;
            }

            // Try to get from input field (if management UI has one)
            if (!key) {
                const keyInput = document.querySelector('input[name="apiKey"]') ||
                                document.querySelector('input[name="managementKey"]') ||
                                document.querySelector('#apiKey') ||
                                document.querySelector('#managementKey');
                if (keyInput) {
                    key = keyInput.value;
                }
            }

            return key || '';
        },

        /**
         * Setup auto-refresh timer
         */
        setupAutoRefresh: function() {
            if (this.state.refreshTimer) {
                clearInterval(this.state.refreshTimer);
            }

            this.state.refreshTimer = setInterval(() => {
                this.fetchQuota();
            }, this.config.refreshInterval);
        },

        /**
         * Cleanup and destroy the module
         */
        destroy: function() {
            if (this.state.refreshTimer) {
                clearInterval(this.state.refreshTimer);
                this.state.refreshTimer = null;
            }

            const container = document.getElementById(this.config.containerId);
            if (container) {
                container.remove();
            }

            this.state.isInitialized = false;
        }
    };

    // Expose module to window
    window.AntigravityQuotaModule = AntigravityQuotaModule;

    // Auto-initialize when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', function() {
            AntigravityQuotaModule.init();
        });
    } else {
        // DOM already loaded, init immediately
        AntigravityQuotaModule.init();
    }

})(window);
