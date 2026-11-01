module github.com/chronobrief/freshcache

// The go directive and the bifrost core requirement are set during /cb-recon,
// because a Go plugin must match the gateway's toolchain and core version exactly.
// internal/decide has no Bifrost imports, so its tests run on any recent Go.
go 1.22
