<?php
// MegoPanel sign-on v2 never accepts bearer tokens in query strings.
header('Cache-Control: no-store, private');
header('Referrer-Policy: no-referrer');
header('Location: ./');
exit;
