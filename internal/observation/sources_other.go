//go:build !darwin

package observation

import "context"

func ProcessFiles(ctx context.Context, identity Identity) ([]string, string) { return nil, "external" }
