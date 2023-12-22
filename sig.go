package main

import (
	"strings"
)

func (bot *robot) getSigOfRepo(org, repo string, cfg *botConfig) (string, error) {
	sigName, err := bot.findSigName(org, repo, cfg, true)
	if err != nil {
		return sigName, err
	}

	return sigName, nil
}

func (bot *robot) listAllFilesOfRepo(cfg *botConfig) (map[string]string, error) {
	trees, err := bot.gic.GetDirectoryTree(cfg.CommunityName, cfg.CommunityRepo, cfg.Branch, 1)

	tree := trees.Tree
	if err != nil || len(tree) == 0 {
		return nil, err
	}

	r := make(map[string]string)
	count := 4

	for i := range tree {
		item := tree[i]
		if strings.Count(item.Path, "/") == count {
			r[item.Path] = strings.Split(item.Path, "/")[1]
		}
	}

	return r, nil
}

func (bot *robot) findSigName(org, repo string, cfg *botConfig, needRefreshTree bool) (sigName string, err error) {
	if len(cfg.reposSig) == 0 {
		files, err := bot.listAllFilesOfRepo(cfg)
		if err != nil {
			return "", err
		}

		cfg.reposSig = files
	}

	for i := range cfg.reposSig {
		if strings.Split(i, "/")[2] == org && strings.Split(strings.Split(i, "/")[4], ".yaml")[0] == repo {
			sigName = cfg.reposSig[i]
			needRefreshTree = false

			break
		}
	}

	if needRefreshTree {
		files, err := bot.listAllFilesOfRepo(cfg)
		if err != nil {
			return "", err
		}

		cfg.reposSig = files

		sigName = bot.fillData(cfg.reposSig, org, repo)
	}

	return sigName, nil
}

func (bot *robot) fillData(reposSig map[string]string, org, repo string) (sigName string) {
	for i := range reposSig {
		if strings.Split(i, "/")[2] == org && strings.Split(strings.Split(i, "/")[4], ".yaml")[0] == repo {
			sigName = reposSig[i]

			break
		}
	}

	return sigName
}
