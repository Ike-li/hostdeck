//go:build !darwin && !windows

package listenerview

import "errors"

func (osOperator) Term(pid int) error { return errors.New("not supported") }

func (osOperator) Kill(pid int) error { return errors.New("not supported") }

func (osOperator) Alive(pid int) bool { return false }
