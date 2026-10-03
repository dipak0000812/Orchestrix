package api

import (
	"fmt"
)

func renderLandingHTML(demoKey string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Orchestrix — Asynchronous Job Orchestration Engine</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
  <style>
    :root {
      --bg: #090d16;
      --card-bg: #111827;
      --card-border: #1f2937;
      --text-main: #f3f4f6;
      --text-muted: #9ca3af;
      --primary: #6366f1;
      --primary-hover: #4f46e5;
      --accent: #06b6d4;
      --success: #10b981;
      --warning: #f59e0b;
      --code-bg: #030712;
      --radius: 12px;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background-color: var(--bg);
      color: var(--text-main);
      font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
      line-height: 1.6;
      padding: 2rem 1rem;
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      align-items: center;
    }
    .container {
      width: 100%%;
      max-width: 900px;
      display: flex;
      flex-direction: column;
      gap: 1.75rem;
    }
    header {
      text-align: center;
      padding: 1.5rem 0;
    }
    .badge-bar {
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      background: rgba(16, 185, 129, 0.1);
      border: 1px solid rgba(16, 185, 129, 0.3);
      padding: 0.35rem 0.85rem;
      border-radius: 9999px;
      font-size: 0.8rem;
      font-weight: 600;
      color: var(--success);
      margin-bottom: 1rem;
    }
    .pulse-dot {
      width: 8px;
      height: 8px;
      background-color: var(--success);
      border-radius: 50%%;
      box-shadow: 0 0 10px var(--success);
      animation: pulse 2s infinite;
    }
    @keyframes pulse {
      0%% { transform: scale(0.95); opacity: 0.8; }
      50%% { transform: scale(1.3); opacity: 1; }
      100%% { transform: scale(0.95); opacity: 0.8; }
    }
    h1 {
      font-size: 2.5rem;
      font-weight: 700;
      letter-spacing: -0.025em;
      background: linear-gradient(135deg, #ffffff 0%%, #c7d2fe 50%%, #818cf8 100%%);
      -webkit-background-clip: text;
      -webkit-text-fill-color: transparent;
      margin-bottom: 0.5rem;
    }
    .subtitle {
      color: var(--text-muted);
      font-size: 1.1rem;
      max-width: 650px;
      margin: 0 auto;
    }
    .card {
      background: var(--card-bg);
      border: 1px solid var(--card-border);
      border-radius: var(--radius);
      padding: 1.5rem;
      box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.3);
    }
    .card-title {
      font-size: 1.15rem;
      font-weight: 600;
      margin-bottom: 0.75rem;
      display: flex;
      align-items: center;
      gap: 0.5rem;
    }
    .demo-key-box {
      background: rgba(99, 102, 241, 0.08);
      border: 1px solid rgba(99, 102, 241, 0.25);
      border-radius: 8px;
      padding: 1rem;
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 0.75rem;
    }
    .key-label {
      font-size: 0.8rem;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: #a5b4fc;
      font-weight: 600;
    }
    .key-value {
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.95rem;
      color: #e0e7ff;
      word-break: break-all;
    }
    button.copy-btn {
      background: var(--primary);
      color: white;
      border: none;
      padding: 0.45rem 0.9rem;
      border-radius: 6px;
      font-size: 0.85rem;
      font-weight: 500;
      cursor: pointer;
      transition: all 0.15s ease;
      display: inline-flex;
      align-items: center;
      gap: 0.35rem;
    }
    button.copy-btn:hover {
      background: var(--primary-hover);
      transform: translateY(-1px);
    }
    .tabs {
      display: flex;
      gap: 0.5rem;
      border-bottom: 1px solid var(--card-border);
      padding-bottom: 0.75rem;
      margin-bottom: 1rem;
      overflow-x: auto;
    }
    .tab-btn {
      background: transparent;
      border: 1px solid transparent;
      color: var(--text-muted);
      padding: 0.4rem 0.8rem;
      border-radius: 6px;
      font-size: 0.85rem;
      font-weight: 500;
      cursor: pointer;
      white-space: nowrap;
    }
    .tab-btn.active {
      background: rgba(99, 102, 241, 0.15);
      color: #c7d2fe;
      border-color: rgba(99, 102, 241, 0.3);
    }
    .snippet-box {
      background: var(--code-bg);
      border: 1px solid #1e293b;
      border-radius: 8px;
      padding: 1rem;
      position: relative;
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.85rem;
      color: #e2e8f0;
      overflow-x: auto;
      white-space: pre-wrap;
      word-break: break-all;
    }
    .snippet-box .copy-code-btn {
      position: absolute;
      top: 0.6rem;
      right: 0.6rem;
      background: #1e293b;
      color: #cbd5e1;
      border: 1px solid #334155;
      padding: 0.25rem 0.55rem;
      border-radius: 4px;
      font-size: 0.75rem;
      cursor: pointer;
    }
    .grid-2 {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
      gap: 1rem;
    }
    .defenses-list {
      list-style: none;
      display: flex;
      flex-direction: column;
      gap: 0.6rem;
      font-size: 0.9rem;
    }
    .defenses-list li {
      display: flex;
      align-items: flex-start;
      gap: 0.6rem;
      color: #d1d5db;
    }
    .defenses-list .bullet {
      color: var(--accent);
      font-size: 1.1rem;
      line-height: 1;
    }
    .links-bar {
      display: flex;
      flex-wrap: wrap;
      gap: 0.75rem;
      margin-top: 0.5rem;
    }
    .links-bar a {
      color: #93c5fd;
      text-decoration: none;
      font-size: 0.85rem;
      border: 1px solid #1e3a8a;
      background: rgba(30, 58, 138, 0.2);
      padding: 0.35rem 0.75rem;
      border-radius: 6px;
      transition: all 0.15s ease;
    }
    .links-bar a:hover {
      background: rgba(30, 58, 138, 0.4);
      color: #ffffff;
      border-color: #3b82f6;
    }
    footer {
      text-align: center;
      font-size: 0.8rem;
      color: #6b7280;
      margin-top: 1rem;
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <div class="badge-bar">
        <span class="pulse-dot"></span>
        SYSTEM OPERATIONAL • RENDER + NEON
      </div>
      <h1>Orchestrix</h1>
      <p class="subtitle">A PostgreSQL-backed asynchronous job orchestration engine in Go with explicit state machine transitions, DAG execution, SSRF guards, and adaptive polling.</p>
    </header>

    <div class="card">
      <div class="card-title">🔑 Reviewer Demo Key</div>
      <p style="font-size: 0.9rem; color: var(--text-muted); margin-bottom: 0.75rem;">
        Pass this key in the <code>Authorization: Bearer &lt;key&gt;</code> header to explore the API. (Demo keys are tenant-isolated and guarded from outbound webhook dispatch).
      </p>
      <div class="demo-key-box">
        <div>
          <div class="key-label">Public Demo API Key</div>
          <div class="key-value" id="demoKeyText">%s</div>
        </div>
        <button class="copy-btn" onclick="copyDemoKey()">
          <span id="copyIcon">📋</span> Copy Key
        </button>
      </div>
    </div>

    <div class="card">
      <div class="card-title">⚡ Interactive API Quickstart</div>
      <div class="tabs">
        <button class="tab-btn active" onclick="switchSnippet(0)">1. Create Job</button>
        <button class="tab-btn" onclick="switchSnippet(1)">2. Check Status</button>
        <button class="tab-btn" onclick="switchSnippet(2)">3. List Jobs</button>
        <button class="tab-btn" onclick="switchSnippet(3)">4. Health Check</button>
      </div>

      <div class="snippet-box" id="snippetContainer">
        <button class="copy-code-btn" onclick="copyActiveSnippet()">Copy Command</button>
        <code id="snippetCode">curl -X POST https://orchestrix-3dp3.onrender.com/api/v1/jobs \
  -H "Authorization: Bearer %s" \
  -H "Content-Type: application/json" \
  -d '{"type":"compute_checksum","payload":{"data":"hello orchestrator","work_factor":1}}'</code>
      </div>
    </div>

    <div class="grid-2">
      <div class="card">
        <div class="card-title">🛡️ Security & DoS Defenses</div>
        <ul class="defenses-list">
          <li><span class="bullet">✓</span> <span><strong>SSRF Dial-Time Guard:</strong> Outbound webhook sockets validated against RFC1918, link-local (169.254.x.x), loopback & multicast CIDRs.</span></li>
          <li><span class="bullet">✓</span> <span><strong>Token-Bucket Rate Limiter:</strong> 120 req/min (burst 30) per API key / IP address.</span></li>
          <li><span class="bullet">✓</span> <span><strong>Payload Body Ceiling:</strong> Request bodies strictly capped at 1 MiB via <code>MaxBytesReader</code>.</span></li>
          <li><span class="bullet">✓</span> <span><strong>Work Factor Cap:</strong> Checksum CPU work factor capped at 100,000; Pagination capped at 500.</span></li>
        </ul>
      </div>

      <div class="card">
        <div class="card-title">🔗 Public Endpoints & Docs</div>
        <p style="font-size: 0.85rem; color: var(--text-muted); margin-bottom: 0.75rem;">
          Liveness probes and metrics are open to load balancers and scrapers without authentication:
        </p>
        <div class="links-bar">
          <a href="/healthz" target="_blank">GET /healthz</a>
          <a href="/health" target="_blank">GET /health</a>
          <a href="/metrics" target="_blank">GET /metrics</a>
          <a href="https://github.com/dipak0000812/Orchestrix" target="_blank">GitHub Repository ↗</a>
        </div>
      </div>
    </div>

    <footer>
      Orchestrix • Go 1.24 • PostgreSQL 16 • Built with strict CAS concurrency
    </footer>
  </div>

  <script>
    const key = "%s";
    const snippets = [
      'curl -X POST https://orchestrix-3dp3.onrender.com/api/v1/jobs \\\n  -H "Authorization: Bearer ' + key + '" \\\n  -H "Content-Type: application/json" \\\n  -d \'{"type":"compute_checksum","payload":{"data":"hello orchestrator","work_factor":1}}\'',
      'curl https://orchestrix-3dp3.onrender.com/api/v1/jobs/<JOB_ID> \\\n  -H "Authorization: Bearer ' + key + '"',
      'curl "https://orchestrix-3dp3.onrender.com/api/v1/jobs?state=SUCCEEDED&limit=10" \\\n  -H "Authorization: Bearer ' + key + '"',
      'curl https://orchestrix-3dp3.onrender.com/healthz'
    ];

    let activeIndex = 0;

    function switchSnippet(idx) {
      activeIndex = idx;
      document.querySelectorAll('.tab-btn').forEach((btn, i) => {
        btn.classList.toggle('active', i === idx);
      });
      document.getElementById('snippetCode').textContent = snippets[idx];
    }

    function copyDemoKey() {
      navigator.clipboard.writeText(key).then(() => {
        const btn = document.querySelector('.copy-btn');
        btn.innerHTML = '✓ Copied!';
        setTimeout(() => {
          btn.innerHTML = '<span id="copyIcon">📋</span> Copy Key';
        }, 2000);
      });
    }

    function copyActiveSnippet() {
      navigator.clipboard.writeText(snippets[activeIndex]).then(() => {
        const btn = document.querySelector('.copy-code-btn');
        btn.textContent = 'Copied!';
        setTimeout(() => {
          btn.textContent = 'Copy Command';
        }, 2000);
      });
    }
  </script>
</body>
</html>
`, demoKey, demoKey, demoKey)
}
