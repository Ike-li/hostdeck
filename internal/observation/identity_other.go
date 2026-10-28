//go:build !darwin && !windows

package observation

func ProcessIdentity(pid int) Identity { return Identity{} }
