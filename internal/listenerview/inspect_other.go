//go:build !darwin && !windows

package listenerview

func platformInspect(pid int) inspection {
	return inspection{}
}
