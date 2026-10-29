package process

import "sync"

// ReadPIDs inspects each distinct process once, at most four at a time.
// Results are assembled after the workers finish, so callers can enrich their
// own snapshots without concurrent map or slice writes.
func ReadPIDs[T any](pids []int, read func(int) T) map[int]T {
	seen := make(map[int]bool)
	unique := make([]int, 0, len(pids))
	for _, pid := range pids {
		if !seen[pid] {
			seen[pid] = true
			unique = append(unique, pid)
		}
	}
	values := make([]T, len(unique))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(4, len(unique)) {
		workers.Go(func() {
			for i := range jobs {
				values[i] = read(unique[i])
			}
		})
	}
	for i := range unique {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	result := make(map[int]T, len(unique))
	for i, pid := range unique {
		result[pid] = values[i]
	}
	return result
}
