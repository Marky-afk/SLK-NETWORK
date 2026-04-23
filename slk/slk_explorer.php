<?php
// SLK Explorer - fetches from local node API
$NODE_API = "http://localhost:8080";

function fetchAPI($endpoint) {
    global $NODE_API;
    $ctx = stream_context_create(['http' => ['timeout' => 3]]);
    $data = @file_get_contents($NODE_API . $endpoint, false, $ctx);
    if (!$data) return null;
    return json_decode($data, true);
}

$stats      = fetchAPI('/api/stats')      ?? [];
$trophies   = fetchAPI('/api/trophies')   ?? [];
$leaderboard= fetchAPI('/api/leaderboard')?? [];
$racers     = fetchAPI('/api/racers')     ?? [];

$height       = $stats['height']       ?? 0;
$total_supply = $stats['total_supply'] ?? 2000000000;
$peers        = $stats['peers']        ?? 0;
$my_address   = $stats['my_address']   ?? '-';
$my_balance   = $stats['my_balance']   ?? 0;
$my_username  = $stats['username']     ?? 'ANON-RACER';

// Handle username change
if ($_SERVER['REQUEST_METHOD'] === 'POST' && !empty($_POST['new_username'])) {
    $new_username = preg_replace('/[^A-Za-z0-9\-_]/', '', strtoupper(trim($_POST['new_username'])));
    if (strlen($new_username) >= 3 && strlen($new_username) <= 20) {
        $username_file = getenv('HOME') . '/.slk/username.txt';
        file_put_contents($username_file, $new_username);
        $my_username = $new_username;
        header('Location: /');
        exit;
    }
}
?>
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta http-equiv="refresh" content="10">
<title>SLK Explorer — Proof of Race</title>
<link href="https://fonts.googleapis.com/css2?family=Share+Tech+Mono&family=Orbitron:wght@400;700;900&family=Exo+2:wght@300;400;600&display=swap" rel="stylesheet">
<style>
  :root {
    --bg:       #040a0f;
    --bg2:      #071219;
    --bg3:      #0a1a24;
    --accent:   #00f0ff;
    --accent2:  #ff6b00;
    --gold:     #ffd700;
    --silver:   #c0c0c0;
    --bronze:   #cd7f32;
    --green:    #00ff88;
    --red:      #ff3355;
    --text:     #d0eaf5;
    --muted:    #4a6a7a;
    --border:   #0f2535;
  }

  * { margin:0; padding:0; box-sizing:border-box; }

  body {
    background: var(--bg);
    color: var(--text);
    font-family: 'Exo 2', sans-serif;
    min-height: 100vh;
    overflow-x: hidden;
  }

  /* Animated grid background */
  body::before {
    content: '';
    position: fixed;
    inset: 0;
    background-image:
      linear-gradient(rgba(0,240,255,0.03) 1px, transparent 1px),
      linear-gradient(90deg, rgba(0,240,255,0.03) 1px, transparent 1px);
    background-size: 40px 40px;
    pointer-events: none;
    z-index: 0;
  }

  /* Glow orbs */
  body::after {
    content: '';
    position: fixed;
    top: -200px; left: -200px;
    width: 600px; height: 600px;
    background: radial-gradient(circle, rgba(0,240,255,0.04) 0%, transparent 70%);
    pointer-events: none;
    z-index: 0;
  }

  .wrap { max-width: 1300px; margin: 0 auto; padding: 0 24px; position: relative; z-index: 1; }

  /* ── HEADER ── */
  header {
    border-bottom: 1px solid var(--border);
    padding: 20px 0;
    background: linear-gradient(180deg, rgba(0,240,255,0.05) 0%, transparent 100%);
  }

  .header-inner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 16px;
  }

  .logo {
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .logo-icon {
    width: 48px; height: 48px;
    background: linear-gradient(135deg, var(--accent), #0060ff);
    border-radius: 12px;
    display: flex; align-items: center; justify-content: center;
    font-size: 24px;
    box-shadow: 0 0 20px rgba(0,240,255,0.3);
  }

  .logo-text h1 {
    font-family: 'Orbitron', monospace;
    font-size: 22px;
    font-weight: 900;
    color: var(--accent);
    letter-spacing: 3px;
    text-shadow: 0 0 20px rgba(0,240,255,0.5);
  }

  .logo-text p {
    font-size: 11px;
    color: var(--muted);
    letter-spacing: 2px;
    text-transform: uppercase;
  }

  .live-badge {
    display: flex;
    align-items: center;
    gap: 8px;
    background: rgba(0,255,136,0.08);
    border: 1px solid rgba(0,255,136,0.2);
    border-radius: 20px;
    padding: 6px 14px;
    font-size: 12px;
    color: var(--green);
    font-family: 'Share Tech Mono', monospace;
  }

  .live-dot {
    width: 8px; height: 8px;
    background: var(--green);
    border-radius: 50%;
    animation: pulse 1.5s infinite;
    box-shadow: 0 0 8px var(--green);
  }

  @keyframes pulse {
    0%, 100% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.5; transform: scale(0.8); }
  }

  /* ── STATS BAR ── */
  .stats-bar {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 16px;
    padding: 28px 0;
  }

  .stat-card {
    background: var(--bg2);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 20px;
    position: relative;
    overflow: hidden;
    transition: border-color 0.3s;
  }

  .stat-card::before {
    content: '';
    position: absolute;
    top: 0; left: 0; right: 0;
    height: 2px;
    background: linear-gradient(90deg, transparent, var(--accent), transparent);
  }

  .stat-card:hover { border-color: rgba(0,240,255,0.3); }

  .stat-label {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 2px;
    color: var(--muted);
    margin-bottom: 8px;
  }

  .stat-value {
    font-family: 'Orbitron', monospace;
    font-size: 22px;
    font-weight: 700;
    color: var(--accent);
    word-break: break-all;
    overflow-wrap: break-word;
  }

  .stat-sub {
    font-size: 11px;
    color: var(--muted);
    margin-top: 4px;
    font-family: 'Share Tech Mono', monospace;
  }

  /* ── SECTION HEADERS ── */
  .section { margin-bottom: 40px; }

  .section-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
    padding-bottom: 12px;
    border-bottom: 1px solid var(--border);
  }

  .section-header h2 {
    font-family: 'Orbitron', monospace;
    font-size: 14px;
    font-weight: 700;
    letter-spacing: 3px;
    text-transform: uppercase;
    color: var(--text);
  }

  .section-icon {
    font-size: 18px;
  }

  .section-count {
    margin-left: auto;
    background: rgba(0,240,255,0.1);
    border: 1px solid rgba(0,240,255,0.2);
    color: var(--accent);
    font-family: 'Share Tech Mono', monospace;
    font-size: 11px;
    padding: 3px 10px;
    border-radius: 20px;
  }

  /* ── LEADERBOARD ── */
  .leaderboard { display: flex; flex-direction: column; gap: 8px; }

  .lb-row {
    display: grid;
    grid-template-columns: 50px 1fr auto auto;
    align-items: center;
    gap: 16px;
    background: var(--bg2);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 14px 20px;
    transition: all 0.2s;
  }

  .lb-row:hover {
    border-color: rgba(0,240,255,0.2);
    background: var(--bg3);
    transform: translateX(4px);
  }

  .lb-rank {
    font-family: 'Orbitron', monospace;
    font-size: 20px;
    font-weight: 900;
    text-align: center;
  }

  .rank-1 { color: var(--gold); text-shadow: 0 0 10px rgba(255,215,0,0.5); }
  .rank-2 { color: var(--silver); }
  .rank-3 { color: var(--bronze); }
  .rank-other { color: var(--muted); font-size: 14px; }

  .lb-address {
    font-family: 'Share Tech Mono', monospace;
    font-size: 13px;
    color: var(--text);
  }

  .lb-address span {
    color: var(--muted);
    font-size: 11px;
    display: block;
    margin-top: 2px;
  }

  .lb-trophies {
    text-align: right;
  }

  .lb-trophies .num {
    font-family: 'Orbitron', monospace;
    font-size: 18px;
    color: var(--accent2);
  }

  .lb-trophies .label {
    font-size: 10px;
    color: var(--muted);
    letter-spacing: 1px;
  }

  .lb-balance {
    text-align: right;
    min-width: 120px;
  }

  .lb-balance .num {
    font-family: 'Share Tech Mono', monospace;
    font-size: 14px;
    color: var(--green);
  }

  .lb-balance .label {
    font-size: 10px;
    color: var(--muted);
    letter-spacing: 1px;
  }

  /* ── TROPHIES TABLE ── */
  .trophy-table {
    width: 100%;
    border-collapse: separate;
    border-spacing: 0 6px;
  }

  .trophy-table th {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 2px;
    color: var(--muted);
    text-align: left;
    padding: 0 16px 8px;
  }

  .trophy-table td {
    background: var(--bg2);
    padding: 12px 16px;
    font-family: 'Share Tech Mono', monospace;
    font-size: 12px;
    border-top: 1px solid var(--border);
    border-bottom: 1px solid var(--border);
    transition: background 0.2s;
  }

  .trophy-table td:first-child {
    border-left: 1px solid var(--border);
    border-radius: 8px 0 0 8px;
  }

  .trophy-table td:last-child {
    border-right: 1px solid var(--border);
    border-radius: 0 8px 8px 0;
  }

  .trophy-table tr:hover td { background: var(--bg3); }

  .hash-cell {
    color: var(--muted);
    font-size: 11px;
  }

  .hash-cell span {
    color: var(--accent);
  }

  .tier-gold   { color: var(--gold); }
  .tier-silver { color: var(--silver); }
  .tier-bronze { color: var(--bronze); }

  .height-badge {
    background: rgba(0,240,255,0.1);
    border: 1px solid rgba(0,240,255,0.2);
    color: var(--accent);
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 11px;
  }

  /* ── LIVE RACERS ── */
  .racers-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 12px;
  }

  .racer-card {
    background: var(--bg2);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 16px;
    position: relative;
    overflow: hidden;
  }

  .racer-card.racing {
    border-color: rgba(0,255,136,0.3);
    box-shadow: 0 0 20px rgba(0,255,136,0.05);
  }

  .racer-card::after {
    content: '';
    position: absolute;
    bottom: 0; left: 0;
    height: 2px;
    background: linear-gradient(90deg, var(--green), transparent);
    width: var(--progress, 50%);
    transition: width 1s;
  }

  .racer-addr {
    font-family: 'Share Tech Mono', monospace;
    font-size: 12px;
    color: var(--accent);
    margin-bottom: 10px;
  }

  .racer-stats {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 8px;
  }

  .racer-stat-item { text-align: center; }

  .racer-stat-val {
    font-family: 'Orbitron', monospace;
    font-size: 14px;
    font-weight: 700;
  }

  .racer-stat-lbl {
    font-size: 9px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--muted);
    margin-top: 2px;
  }

  .status-racing  { color: var(--green); }
  .status-cooling { color: var(--accent); }
  .status-joined  { color: var(--accent2); }

  /* ── EMPTY STATE ── */
  .empty {
    text-align: center;
    padding: 40px;
    color: var(--muted);
    font-family: 'Share Tech Mono', monospace;
    font-size: 13px;
    border: 1px dashed var(--border);
    border-radius: 12px;
  }

  /* ── FOOTER ── */
  footer {
    border-top: 1px solid var(--border);
    padding: 24px 0;
    margin-top: 40px;
    text-align: center;
    font-size: 11px;
    color: var(--muted);
    font-family: 'Share Tech Mono', monospace;
    letter-spacing: 1px;
  }

  footer span { color: var(--accent); }

  /* ── TWO COLUMN LAYOUT ── */
  .two-col {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 32px;
  }

  @media (max-width: 900px) {
    .two-col { grid-template-columns: 1fr; }
    .lb-row { grid-template-columns: 40px 1fr auto; }
    .lb-balance { display: none; }
  }

  /* refresh notice */
  .refresh-note {
    font-size: 10px;
    color: var(--muted);
    font-family: 'Share Tech Mono', monospace;
    letter-spacing: 1px;
  }
</style>
</head>
<body>

<header>
  <div class="wrap">
    <div class="header-inner">
      <div class="logo">
        <div class="logo-icon">🏁</div>
        <div class="logo-text">
          <h1>SLK EXPLORER</h1>
          <p>Proof of Race Blockchain</p>
        </div>
      </div>
      <div style="display:flex;align-items:center;gap:16px;flex-wrap:wrap;">
        <div class="live-badge">
          <div class="live-dot"></div>
          LIVE · <?= $peers ?> PEERS
        </div>
        <div class="live-badge" style="border-color:rgba(255,107,0,0.3);background:rgba(255,107,0,0.08);color:var(--accent2);">
          🏷️ <?= htmlspecialchars($my_username) ?>
        </div>
        <div class="refresh-note">AUTO REFRESH 10s</div>
      </div>
    </div>
  </div>
</header>

<div class="wrap">

  <!-- STATS BAR -->
  <div class="stats-bar">
    <div class="stat-card">
      <div class="stat-label">Block Height</div>
      <div class="stat-value"><?= number_format($height) ?></div>
      <div class="stat-sub">Trophies mined</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Total Supply</div>
      <div class="stat-value" style="font-size:13px;"><?= number_format($total_supply, 3) ?></div>
      <div class="stat-sub">SLK remaining</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Mined So Far</div>
      <div class="stat-value" style="font-size:18px;"><?= number_format(2000000000 - $total_supply, 3) ?></div>
      <div class="stat-sub">SLK distributed</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Active Peers</div>
      <div class="stat-value"><?= $peers ?></div>
      <div class="stat-sub">Connected nodes</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Block Reward</div>
      <div class="stat-value">0.008</div>
      <div class="stat-sub">SLK per trophy</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Live Racers</div>
      <div class="stat-value"><?= count($racers) ?></div>
      <div class="stat-sub">Racing right now</div>
    </div>
  </div>

  <div class="two-col">

    <!-- LEADERBOARD -->
    <div class="section">
      <div class="section-header">
        <span class="section-icon">🏆</span>
        <h2>Leaderboard</h2>
        <span class="section-count"><?= count($leaderboard) ?> miners</span>
      </div>
      <div class="leaderboard">
        <?php if (empty($leaderboard)): ?>
          <div class="empty">No miners yet — be the first!</div>
        <?php else: ?>
          <?php foreach ($leaderboard as $i => $entry): ?>
            <?php
              $rank = $i + 1;
              $rankClass = match($rank) { 1 => 'rank-1', 2 => 'rank-2', 3 => 'rank-3', default => 'rank-other' };
              $rankSymbol = match($rank) { 1 => '🥇', 2 => '🥈', 3 => '🥉', default => "#$rank" };
            ?>
            <div class="lb-row">
              <div class="lb-rank <?= $rankClass ?>"><?= $rankSymbol ?></div>
              <div class="lb-address">
                <?= htmlspecialchars(substr($entry['address'], 0, 20)) ?>...
                <span><?= htmlspecialchars($entry['address']) ?></span>
              </div>
              <div class="lb-trophies">
                <div class="num"><?= $entry['trophies'] ?></div>
                <div class="label">TROPHIES</div>
              </div>
              <div class="lb-balance">
                <div class="num"><?= number_format($entry['balance'], 5) ?> SLK</div>
                <div class="label">EARNED</div>
              </div>
            </div>
          <?php endforeach; ?>
        <?php endif; ?>
      </div>
    </div>

    <!-- LIVE RACERS -->
    <div class="section">
      <div class="section-header">
        <span class="section-icon">🏎️</span>
        <h2>Live Racers</h2>
        <span class="section-count"><?= count($racers) ?> racing</span>
      </div>
      <?php if (empty($racers)): ?>
        <div class="empty">No active racers right now</div>
      <?php else: ?>
        <div class="racers-grid">
          <?php foreach ($racers as $racer): ?>
            <?php
              $statusClass = match(strtoupper($racer['status'] ?? '')) {
                'RACING' => 'status-racing',
                'COOLING' => 'status-cooling',
                default => 'status-joined'
              };
            ?>
            <div class="racer-card <?= strtolower($racer['status'] ?? '') ?>">
              <div class="racer-addr">
                <?= !empty($racer['username']) ? htmlspecialchars($racer['username']) : htmlspecialchars(substr($racer['address'], 0, 20)) ?>
                <span style="font-size:10px;color:var(--muted);display:block;"><?= htmlspecialchars($racer['address']) ?></span>
              </div>
              <div class="racer-stats">
                <div class="racer-stat-item">
                  <div class="racer-stat-val <?= $statusClass ?>"><?= number_format($racer['distance_left'], 1) ?>m</div>
                  <div class="racer-stat-lbl">Dist Left</div>
                </div>
                <div class="racer-stat-item">
                  <div class="racer-stat-val" style="color:var(--accent2)"><?= number_format($racer['power'], 1) ?>W</div>
                  <div class="racer-stat-lbl">Power</div>
                </div>
                <div class="racer-stat-item">
                  <div class="racer-stat-val" style="color:<?= $racer['temp'] >= 90 ? 'var(--red)' : ($racer['temp'] >= 80 ? 'var(--accent2)' : 'var(--green)') ?>"><?= number_format($racer['temp'], 0) ?>°C</div>
                  <div class="racer-stat-lbl">Temp</div>
                </div>
              </div>
            </div>
          <?php endforeach; ?>
        </div>
      <?php endif; ?>
    </div>

  </div>

  <!-- TROPHY CHAIN -->
  <div class="section">
    <div class="section-header">
      <span class="section-icon">⛓️</span>
      <h2>Trophy Chain</h2>
      <span class="section-count"><?= count($trophies) ?> blocks</span>
    </div>
    <?php if (empty($trophies)): ?>
      <div class="empty">No trophies yet — start racing to mine the first block!</div>
    <?php else: ?>
      <?php
        $perPage   = 10;
        $reversed  = array_reverse($trophies);
        $totalPages = max(1, ceil(count($reversed) / $perPage));
        $page      = max(1, min($totalPages, (int)($_GET['page'] ?? 1)));
        $paginated = array_slice($reversed, ($page - 1) * $perPage, $perPage);
      ?>
      <div style="overflow-x:auto;">
      <table class="trophy-table" style="min-width:900px;">
        <thead>
          <tr>
            <th>Block</th>
            <th>Miner</th>
            <th>Wallet Address</th>
            <th>Distance</th>
            <th>Time</th>
            <th>Tier</th>
            <th>Reward</th>
            <th>Hash</th>
          </tr>
        </thead>
        <tbody>
          <?php foreach ($paginated as $t): ?>
            <?php
              $tierClass = match(strtolower($t['tier'] ?? 'gold')) {
                'gold'   => 'tier-gold',
                'silver' => 'tier-silver',
                'bronze' => 'tier-bronze',
                default  => 'tier-gold'
              };
              $tierIcon = match(strtolower($t['tier'] ?? 'gold')) {
                'gold'   => '🥇',
                'silver' => '🥈',
                'bronze' => '🥉',
                default  => '🥇'
              };
              $isMe = ($t['winner'] === $my_address);
            ?>
            <tr>
              <td><span class="height-badge">#<?= $t['height'] ?></span></td>
              <td style="font-family:'Share Tech Mono',monospace;font-size:12px;color:var(--accent2)">
                <?= $isMe ? htmlspecialchars($my_username) : '❓ UNKNOWN' ?>
                <?php if ($isMe): ?><span style="font-size:9px;color:var(--green);display:block;">YOUR NODE</span><?php endif; ?>
              </td>
              <td style="font-family:'Share Tech Mono',monospace;font-size:11px;color:var(--accent)">
                <?= htmlspecialchars($t['winner']) ?>
              </td>
              <td><?= number_format($t['distance'], 3) ?>m</td>
              <td><?= number_format($t['time'], 2) ?>s</td>
              <td class="<?= $tierClass ?>"><?= $tierIcon ?> <?= htmlspecialchars($t['tier']) ?></td>
              <td style="color:var(--green)"><?= number_format($t['reward'], 8) ?> SLK</td>
              <td style="font-family:'Share Tech Mono',monospace;font-size:10px;color:var(--muted);word-break:break-all;">
                <span style="color:var(--accent)"><?= substr($t['hash'], 0, 8) ?></span><?= substr($t['hash'], 8) ?>
              </td>
            </tr>
          <?php endforeach; ?>
        </tbody>
      </table>
      </div>
      <?php if ($totalPages > 1): ?>
      <div style="display:flex;justify-content:center;gap:8px;margin-top:16px;flex-wrap:wrap;">
        <?php if ($page > 1): ?>
          <a href="?page=<?= $page-1 ?>" style="background:var(--bg2);border:1px solid var(--border);color:var(--accent);padding:6px 14px;border-radius:6px;font-family:'Share Tech Mono',monospace;font-size:12px;text-decoration:none;">← Prev</a>
        <?php endif; ?>
        <?php for ($p = 1; $p <= $totalPages; $p++): ?>
          <a href="?page=<?= $p ?>" style="background:<?= $p==$page ? 'rgba(0,240,255,0.15)' : 'var(--bg2)' ?>;border:1px solid <?= $p==$page ? 'rgba(0,240,255,0.4)' : 'var(--border)' ?>;color:var(--accent);padding:6px 12px;border-radius:6px;font-family:'Share Tech Mono',monospace;font-size:12px;text-decoration:none;"><?= $p ?></a>
        <?php endfor; ?>
        <?php if ($page < $totalPages): ?>
          <a href="?page=<?= $page+1 ?>" style="background:var(--bg2);border:1px solid var(--border);color:var(--accent);padding:6px 14px;border-radius:6px;font-family:'Share Tech Mono',monospace;font-size:12px;text-decoration:none;">Next →</a>
        <?php endif; ?>
      </div>
      <div style="text-align:center;margin-top:8px;font-family:'Share Tech Mono',monospace;font-size:11px;color:var(--muted);">
        Page <?= $page ?> of <?= $totalPages ?> · <?= count($trophies) ?> total trophies
      </div>
      <?php endif; ?>
    <?php endif; ?>
  </div>

</div>

<footer>
  <div class="wrap">
    SLK PROOF-OF-RACE BLOCKCHAIN · RACER <span><?= htmlspecialchars($my_username) ?></span> · NODE <span><?= htmlspecialchars($my_address) ?></span> · BALANCE <span><?= number_format($my_balance, 8) ?> SLK</span>
    <br><br>
    Built with real cryptography · Ed25519 signatures · SHA-256 VDF · libp2p P2P network
    <br><br>
    <form method="POST" style="display:inline-flex;gap:8px;align-items:center;margin-top:8px;">
      <input type="text" name="new_username" placeholder="Change username..." maxlength="20"
        style="background:#071219;border:1px solid #0f2535;color:#00f0ff;padding:6px 12px;border-radius:6px;font-family:'Share Tech Mono',monospace;font-size:12px;outline:none;">
      <button type="submit"
        style="background:rgba(0,240,255,0.1);border:1px solid rgba(0,240,255,0.3);color:#00f0ff;padding:6px 14px;border-radius:6px;font-family:'Share Tech Mono',monospace;font-size:12px;cursor:pointer;">
        ✏️ Change
      </button>
    </form>
  </div>
</footer>

</body>
</html>
