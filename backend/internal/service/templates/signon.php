<?php
// MegoPanel sign-on bridge v2. The one-time token is supplied in the URL
// fragment, which browsers never send to web or proxy access logs.
$isSecure = isset($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== '' && $_SERVER['HTTPS'] !== 'off';
$scheme = $isSecure ? 'https' : 'http';
$expectedOrigin = $scheme . '://' . ($_SERVER['HTTP_HOST'] ?? '');

header('Cache-Control: no-store, private');
header('Pragma: no-cache');
header('Referrer-Policy: no-referrer');
header('X-Content-Type-Options: nosniff');
header('X-Frame-Options: DENY');

if (($_GET['action'] ?? '') === 'logout') {
    session_name('SignonSession');
    session_set_cookie_params(['path' => '/', 'secure' => $isSecure, 'httponly' => true, 'samesite' => 'Lax']);
    session_start();
    $_SESSION = [];
    if (ini_get('session.use_cookies')) {
        $params = session_get_cookie_params();
        setcookie(session_name(), '', time() - 42000, $params['path'], $params['domain'], $params['secure'], $params['httponly']);
    }
    session_destroy();
    header('Location: ./');
    exit;
}

if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    $origin = $_SERVER['HTTP_ORIGIN'] ?? '';
    if (!hash_equals($expectedOrigin, $origin)) {
        http_response_code(403);
        exit('Forbidden');
    }

    $token = $_POST['token'] ?? '';
    if (!is_string($token) || !preg_match('/\A[0-9a-fA-F-]{36}\z/', $token)) {
        http_response_code(403);
        exit('Invalid or expired sign-on token');
    }

    $payload = json_encode(['token' => $token]);
    $curl = curl_init('http://127.0.0.1:__MEGOPANEL_API_PORT__/internal/phpmyadmin/token');
    curl_setopt_array($curl, [
        CURLOPT_POST => true,
        CURLOPT_POSTFIELDS => $payload,
        CURLOPT_HTTPHEADER => ['Content-Type: application/json'],
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_CONNECTTIMEOUT => 2,
        CURLOPT_TIMEOUT => 5,
    ]);
    $response = curl_exec($curl);
    $status = curl_getinfo($curl, CURLINFO_HTTP_CODE);
    curl_close($curl);

    $credentials = is_string($response) ? json_decode($response, true) : null;
    if ($status !== 200 || !is_array($credentials) || !isset($credentials['username'], $credentials['password']) ||
        !is_string($credentials['username']) || !is_string($credentials['password'])) {
        http_response_code(403);
        exit('Sign-on failed. Return to MegoPanel and try again.');
    }

    session_name('SignonSession');
    session_set_cookie_params(['path' => '/', 'secure' => $isSecure, 'httponly' => true, 'samesite' => 'Lax']);
    session_start();
    session_regenerate_id(true);
    $_SESSION['PMA_single_signon_user'] = $credentials['username'];
    $_SESSION['PMA_single_signon_password'] = $credentials['password'];
    $_SESSION['PMA_single_signon_host'] = 'localhost';
    session_write_close();
    http_response_code(204);
    exit;
}

$nonce = bin2hex(random_bytes(16));
header("Content-Security-Policy: default-src 'none'; script-src 'nonce-" . $nonce . "'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'");
header('Content-Type: text/html; charset=utf-8');
?>
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Signing in to phpMyAdmin</title></head>
<body>
<p id="status">Signing in to phpMyAdmin…</p>
<script nonce="<?= htmlspecialchars($nonce, ENT_QUOTES, 'UTF-8') ?>">
(() => {
  const token = location.hash.slice(1);
  history.replaceState(null, '', location.pathname);
  const status = document.getElementById('status');
  if (!/^[0-9a-fA-F-]{36}$/.test(token)) {
    status.textContent = 'The sign-on link is missing or expired. Return to MegoPanel and try again.';
    return;
  }
  fetch('./signon.php', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ token })
  }).then(response => {
    if (!response.ok) throw new Error('Sign-on failed');
    location.replace('./index.php');
  }).catch(() => {
    status.textContent = 'Sign-on failed. Return to MegoPanel and try again.';
  });
})();
</script>
</body>
</html>
