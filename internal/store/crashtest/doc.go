// Package crashtest kills a process that writes to a database with SIGKILL
// at random moments and checks what survives (§7.3, §9.5, plan/TESTING.md
// §7). The tests need the crash build tag: `make crash`.
package crashtest
