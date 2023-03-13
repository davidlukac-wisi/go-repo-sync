package main

/*
Copyright © 2023 David Lukac <1215290+davidlukac@users.noreply.github.com>
Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:
The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.
THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

import (
	"fmt"
	"os"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Remote - struct for reading info about remote from input YAML.
type Remote struct {
	Name string `yaml:"name"`
	Url  string `yaml:"url,omitempty"`
}

// Repo - struct for reading repository info from input YAML.
type Repo struct {
	Name         string
	Path         string  `yaml:"path"`
	SourceRemote *Remote `yaml:"sourceRemote"`
	TargetRemote *Remote `yaml:"targetRemote"`
}

// RepoSync - struct for reading sync info from input YAML.
type RepoSync struct {
	Repos         map[string]*Repo  `yaml:"repos"`
	BranchMapping map[string]string `yaml:"branchMapping"`
}

// readInput - Read info about syncing repositories from input YAML file. Returns RepoSync struct.
func (rs *RepoSync) readInput(path string) (*RepoSync, error) {
	yamlFile, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read Yaml file '%s': %v ", path, err)
		return nil, err
	}

	err = yaml.Unmarshal(yamlFile, &rs)
	if err != nil {
		log.Fatalf("failed to unmarshal input: %v", err)
	}

	for k, v := range rs.Repos {
		v.Name = k
	}

	return rs, nil
}

// mapBranch - Return mapped branches from read RepoSync info, or the same name if there's no mapping.
func (rs *RepoSync) mapBranch(branchName string) string {
	if v, ok := rs.BranchMapping[branchName]; ok {
		return v
	} else {
		return branchName
	}
}

// repoGetLocalBranchForRemote - Checks if repo already has checked out remote branch, if yes, return reference to respective local branch,
// else return nil.
func repoGetLocalBranchForRemote(repo *git.Repository, remoteBranch *plumbing.Reference) (*plumbing.Reference, error) {
	branches, err := repo.Branches()
	if err != nil {
		log.Errorf("failed to get branches: %v", err)
	}

	localBranches := []*plumbing.Reference{}

	branches.ForEach(func(r *plumbing.Reference) error {
		if r.Name().IsBranch() && strings.HasPrefix(string(r.Name()), "refs/heads/") {
			localBranches = append(localBranches, r)
		}

		return nil
	})

	var foundLocalBranch *plumbing.Reference

	branches, err = repo.Branches()
	if err != nil {
		log.Errorf("failed to get branches: %v", err)
	}
	branches.ForEach(func(localBranch *plumbing.Reference) error {
		if localBranch.Name().String() == remoteBranch.Name().String() {
			foundLocalBranch = localBranch
			return nil
		}
		return nil
	})

	return foundLocalBranch, err
}

func main() {
	var repoSync *RepoSync
	repoSync, err := repoSync.readInput(os.Args[1])
	if err != nil {
		panic(err.Error())
	}

	for _, rs := range repoSync.Repos {
		log.Infof("Opening %s...", rs.Path)
		repo, err := git.PlainOpen(rs.Path)
		if err != nil {
			log.Errorf("failed to open repo from %s: %v", rs.Path, err)
			os.Exit(1)
		}

		remotes, err := repo.Remotes()
		if err != nil {
			log.Errorf("failed to get remotes for %s: %v", rs.Path, err)
			os.Exit(1)
		}

		// Add target remote if doesn't exist.
		_, err = repo.Remote(rs.TargetRemote.Name)
		if err != nil {
			log.Infof("Target remote %s missing for '%s' ... adding %s", rs.TargetRemote.Name, rs.Path, rs.TargetRemote.Url)
			repo.CreateRemote(&config.RemoteConfig{
				Name: rs.TargetRemote.Name,
				URLs: []string{rs.TargetRemote.Url},
			})
		}

		var branchesToSync []*plumbing.Reference

		// Fetch everything.
		for _, remote := range remotes {
			log.Infof("Found remote '%s' in '%s' repo... fetching", remote.Config().Name, rs.Path)
			err = remote.Fetch(&git.FetchOptions{
				RemoteName: remote.String(),
				Tags:       git.AllTags,
			})
			if err != nil && err != git.NoErrAlreadyUpToDate {
				log.Errorf("failed to fetch %s in '%s' repo: %v", remote.Config().Name, rs.Path, err)
				os.Exit(1)
			}

			if remote.Config().Name == rs.SourceRemote.Name {
				remoteRefs, err := remote.List(&git.ListOptions{})
				if err != nil {
					log.Errorf("failed to get remote objects for remote '%s' in repo '%s': %v", remote.Config().Name, rs.Path, err)
					os.Exit(1)
				}

				for _, r := range remoteRefs {
					if r.Name().IsBranch() {
						log.Infof("Found remote branch '%s' for remote '%s' in repo '%s'.", r.Name(), remote.Config().Name, rs.Path)
						branchesToSync = append(branchesToSync, r)
					}
				}
			}
		}

		log.Infof("Branches to sync: %v", branchesToSync)
		for _, remoteBranch := range branchesToSync {
			w, err := repo.Worktree()
			if err != nil {
				log.Errorf("failed to get working tree for repository %s: %v", rs.Path, err)
				os.Exit(1)
			}

			localBranch, err := repoGetLocalBranchForRemote(repo, remoteBranch)
			if err != nil {
				log.Errorf("failed to determine whether repo %v already had local copy of branch %s in %s", repo, remoteBranch, rs.Path)
				os.Exit(1)
			}

			if localBranch == nil {
				log.Infof("Checking out branch %s in %s", remoteBranch.Name().Short(), rs.Path)
				err = w.Checkout(&git.CheckoutOptions{
					Hash:   remoteBranch.Hash(),
					Branch: remoteBranch.Name(),
					Create: true,
					Force:  true,
					Keep:   false,
				})
				if err != nil {
					log.Errorf("failed to checkout %s in %s: %v", remoteBranch.Name().Short(), rs.Path, err)
					os.Exit(1)
				}
				localBranch, err = repo.Head()
				if err != nil {
					log.Errorf("failed to get branch HEAD after checkout: %v", err)
					os.Exit(1)
				}
				if localBranch.Hash() != remoteBranch.Hash() || localBranch.Name() != remoteBranch.Name() {
					log.Errorf("failed to check out branch correctly: %s vs %s; %s vs %s",
						localBranch.Hash(), remoteBranch.Hash(), localBranch.Name(), remoteBranch.Name())
					os.Exit(1)
				}
			} else {
				log.Infof("Switching to branch %s in %s", localBranch.Name().Short(), rs.Path)
				err = w.Checkout(&git.CheckoutOptions{
					Branch: localBranch.Name(),
					Create: false,
					Force:  true,
					Keep:   false,
				})
				if err != nil {
					log.Errorf("failed to switch to %s in %s: %v", localBranch.Name().Short(), rs.Path, err)
					os.Exit(1)
				}
			}

			log.Infof("Pulling %s from '%s' of %s", remoteBranch.Name().Short(), rs.SourceRemote.Name, rs.Path)
			err = w.Pull(&git.PullOptions{
				RemoteName:    rs.SourceRemote.Name,
				ReferenceName: remoteBranch.Name(),
				SingleBranch:  true,
				Force:         true,
			})
			if err != nil && err != git.NoErrAlreadyUpToDate {
				log.Errorf("failed to pull %s in %s: %v", remoteBranch.Name().Short(), rs.Path, err)
				os.Exit(1)
			}

			log.Infof("Reseting branch %s to %s", localBranch.Name().Short(), localBranch.Hash())
			err = w.Reset(&git.ResetOptions{
				Commit: localBranch.Hash(),
				Mode:   git.HardReset,
			})
			if err != nil {
				log.Errorf("failed to reset branch %s in %s: %v", remoteBranch.Name().Short(), rs.Path, err)
				os.Exit(1)
			}

			requiredRefSpecStr := fmt.Sprintf(
				"+%s:refs/heads/%s",
				localBranch.Name().String(),
				repoSync.mapBranch(remoteBranch.Name().Short()),
			)
			refSpec := config.RefSpec(requiredRefSpecStr)
			log.Infof("Pushing %s", refSpec)
			err = repo.Push(&git.PushOptions{
				RemoteName: rs.TargetRemote.Name,
				Force:      true,
				RefSpecs:   []config.RefSpec{refSpec},
				Atomic:     true,
			})
			if err != nil {
				if err == git.NoErrAlreadyUpToDate {
					log.Infof("remote up to date - %s", requiredRefSpecStr)
				} else {
					log.Errorf("failed to push %s: %v", requiredRefSpecStr, err)
					os.Exit(1)
				}
			}

			// pushTarget := fmt.Sprintf("%s:%s", localBranch.Name().Short(), repoSync.mapBranch(remoteBranch.Name().Short()))
			// cmdLst := []string{"git", "push", "-u", rs.TargetRemote.Name, pushTarget, "--force"}
			// log.Info("Pushing with %v", cmdLst)
			// cmd := exec.Command(cmdLst[0], cmdLst[1:]...)
			// cmd.Dir = rs.Path
			// _, err = cmd.Output()
			// if err != nil {
			// 	log.Errorf("failed to push %s to %s in %s: %v; error was %s", localBranch.Name().Short(), pushTarget, rs.TargetRemote.Name, err, cmd.Stderr)
			// }

			status, err := w.Status()
			if err != nil {
				log.Errorf("failed to get repo status: %v", err)
				os.Exit(1)
			} else {
				log.Infof("Repository status: %v", status)
			}

		}

		// Push all tags
		tags, err := repo.Tags()
		if err != nil {
			log.Errorf("failed to get tags: %v", err)
			os.Exit(1)
		} else {
			tags.ForEach(func(t *plumbing.Reference) error {
				tagsRefSpec := fmt.Sprintf("+refs/tags/%s:refs/tags/%s", t.Name().Short(), t.Name().Short())
				log.Infof("Pushing tag %s to %s with refspec %s", t.Name().Short(), rs.TargetRemote.Name, tagsRefSpec)
				err = repo.Push(&git.PushOptions{
					RemoteName: rs.TargetRemote.Name,
					RefSpecs:   []config.RefSpec{config.RefSpec(tagsRefSpec)},
					FollowTags: true,
					Force:      true,
				})
				if err != nil {
					if err == git.NoErrAlreadyUpToDate {
						log.Infof("tag %s already up to date", t.Name().Short())
					} else {
						log.Errorf("failed to push tags: %v", err)
						os.Exit(1)
					}
				}

				return nil
			})
		}
	}
}
