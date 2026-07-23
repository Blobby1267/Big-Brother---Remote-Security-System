// Agent module: runs on managed devices and executes relay-issued commands.
module github.com/blobby1267/big-brother-agent

go 1.20

// Secure storage for device private keys across OS keychains.
require github.com/zalando/go-keyring v0.2.8

// Windows service/runtime integration used by the Windows agent service host.
require golang.org/x/sys v0.22.0
