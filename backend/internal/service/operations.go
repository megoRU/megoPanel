package service

import "sync"

// systemOperationMu serializes filesystem and service changes that must not
// overlap, including website changes and backups/restores of those files.
var systemOperationMu sync.Mutex
