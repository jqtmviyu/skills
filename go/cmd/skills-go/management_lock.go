package main

import (
	"github.com/vercel-labs/skills/go/lockfile"
)

type managementLock struct {
	global  *lockfile.GlobalLock
	project *lockfile.ProjectLock
}

func readManagementLock(global bool, cwd string) (map[string]lockfile.Entry, *managementLock, error) {
	if global {
		lock, err := lockfile.ReadGlobal()
		if err != nil {
			return nil, nil, err
		}
		return lock.Skills, &managementLock{global: lock}, nil
	}
	lock, err := lockfile.ReadProject(cwd)
	if err != nil {
		return nil, nil, err
	}
	return lock.Skills, &managementLock{project: lock}, nil
}

func writeManagementLock(global bool, cwd string, lock *managementLock) error {
	if global {
		return lockfile.WriteGlobal(lock.global)
	}
	return lockfile.WriteProject(cwd, lock.project)
}
