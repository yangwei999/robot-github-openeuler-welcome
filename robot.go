package main

import (
	"fmt"
	"strings"

	sdk "github.com/google/go-github/v36/github"
	"github.com/opensourceways/community-robot-lib/config"
	gc "github.com/opensourceways/community-robot-lib/githubclient"
	framework "github.com/opensourceways/community-robot-lib/robot-github-framework"
	"github.com/opensourceways/community-robot-lib/utils"
	"github.com/sirupsen/logrus"
)

const (
	botName        = "welcome"
	welcomeMessage = `
Hi ***%s***, welcome to the %s Community.
I'm the Bot here serving you. You can find the instructions on how to interact with me at **[Here](%s)**.
If you have any questions, please contact the SIG: [%s](https://gitee.com/openeuler/community/tree/master/sig/%s), and any of the maintainers: @%s`
	welcomeMessage2 = `
Hi ***%s***, welcome to the %s Community.
I'm the Bot here serving you. You can find the instructions on how to interact with me at **[Here](%s)**.
If you have any questions, please contact the SIG: [%s](https://gitee.com/openeuler/community/tree/master/sig/%s), and any of the maintainers: @%s or the committers: @%s`
)

type iClient interface {
	AddPRLabel(pr gc.PRInfo, label string) error
	RemovePRLabel(pr gc.PRInfo, label string) error
	CreatePRComment(pr gc.PRInfo, comment string) error
	CreateIssueComment(is gc.PRInfo, comment string) error
	AddIssueLabel(is gc.PRInfo, label []string) error
	ListCollaborator(pr gc.PRInfo) ([]*sdk.User, error)
	GetPathContent(org, repo, path, branch string) (*sdk.RepositoryContent, error)
	GetRepoLabels(org, repo string) ([]string, error)
	CreateRepoLabel(org, repo, label string) error
	GetDirectoryTree(org, repo, branch string, recursive bool) ([]*sdk.TreeEntry, error)
}

func newRobot(cli iClient) *robot {
	return &robot{cli: cli}
}

type robot struct {
	cli iClient
}

func (bot *robot) NewConfig() config.Config {
	return &configuration{}
}

func (bot *robot) getConfig(cfg config.Config, org, repo string) (*botConfig, error) {
	c, ok := cfg.(*configuration)
	if !ok {
		return nil, fmt.Errorf("can't convert to configuration")
	}

	if bc := c.configFor(org, repo); bc != nil {
		return bc, nil
	}

	return nil, fmt.Errorf("no config for this repo:%s/%s", org, repo)
}

func (bot *robot) RegisterEventHandler(p framework.HandlerRegister) {
	p.RegisterIssueHandler(bot.handleIssueEvent)
	p.RegisterPullRequestHandler(bot.handlePREvent)
}

func (bot *robot) handlePREvent(e *sdk.PullRequestEvent, pc config.Config, log *logrus.Entry) error {
	if e.GetAction() != "opened" {
		return nil
	}

	org, repo := strings.Split(e.Repo.GetFullName(), "/")[0], e.GetRepo().GetName()
	cfg, err := bot.getConfig(pc, org, repo)
	if err != nil {
		return err
	}

	number := *e.Number
	author := e.PullRequest.User.GetLogin()
	pr := gc.PRInfo{Org: org, Repo: repo, Number: number}

	return bot.handle(
		org, repo, author, cfg, log,

		func(c string) error {
			return bot.cli.CreatePRComment(pr, c)
		},

		func(label string) error {
			return bot.cli.AddPRLabel(pr, label)
		},
	)
}

func (bot *robot) handleIssueEvent(e *sdk.IssuesEvent, pc config.Config, log *logrus.Entry) error {
	if e.GetAction() != "opened" {
		return nil
	}

	org, repo := strings.Split(e.Repo.GetFullName(), "/")[0], e.GetRepo().GetName()
	fmt.Println(org, repo)
	cfg, err := bot.getConfig(pc, org, repo)
	if err != nil {
		fmt.Println(err)
		return err
	}

	author := e.GetIssue().GetUser().GetLogin()
	number := e.GetIssue().GetNumber()
	fmt.Println(org, repo, author, number)
	is := gc.PRInfo{
		Org:    org,
		Repo:   repo,
		Number: number,
	}

	return bot.handle(
		org, repo, author, cfg, log,

		func(c string) error {
			return bot.cli.CreateIssueComment(is, c)
		},

		func(label string) error {
			return bot.cli.AddIssueLabel(is, []string{label})
		},
	)
}

func (bot *robot) handle(
	org, repo, author string,
	cfg *botConfig, log *logrus.Entry,
	addMsg, addLabel func(string) error,
) error {
	sigName, comment, err := bot.genComment(org, repo, author, cfg)
	if err != nil {
		return err
	}

	mErr := utils.NewMultiErrors()

	if err := addMsg(comment); err != nil {
		mErr.AddError(err)
	}

	label := fmt.Sprintf("sig/%s", sigName)

	if err := bot.createLabelIfNeed(org, repo, label); err != nil {
		log.Errorf("create repo label:%s, err:%s", label, err.Error())
	}

	if err := addLabel(label); err != nil {
		mErr.AddError(err)
	}

	return mErr.Err()
}

func (bot robot) genComment(org, repo, author string, cfg *botConfig) (string, string, error) {
	sigName, err := bot.getSigOfRepo(org, repo, cfg)
	if err != nil {
		return "", "", err
	}

	if sigName == "" {
		return "", "", fmt.Errorf("cant get sig name of repo: %s/%s", org, repo)
	}

	maintainers, committers, err := bot.getMaintainers(org, repo, sigName)
	if err != nil {
		return "", "", err
	}

	if len(committers) != 0 {
		return sigName, fmt.Sprintf(
			welcomeMessage2, author, cfg.CommunityName, cfg.CommandLink,
			sigName, sigName, strings.Join(maintainers, " , @"), strings.Join(committers, " , @"),
		), nil
	}

	return sigName, fmt.Sprintf(
		welcomeMessage, author, cfg.CommunityName, cfg.CommandLink,
		sigName, sigName, strings.Join(maintainers, " , @"),
	), nil
}

func (bot *robot) getMaintainers(org, repo, sigName string) ([]string, []string, error) {
	v, err := bot.cli.ListCollaborator(gc.PRInfo{Org: org, Repo: repo})
	if err != nil {
		return nil, nil, err
	}

	r := make([]string, 0, len(v))
	for i := range v {
		p := v[i].Permissions
		if p != nil {
			for j := range p {
				if (j == "push" || j == "maintain") && p[j] {
					r = append(r, v[i].GetLogin())
				}
			}
		}
	}

	// check OWNERS file
	_, err = bot.cli.GetPathContent("wanghao75", "community",
		fmt.Sprintf("sig/%s/OWNERS", sigName), "master")
	if err != nil {
		// OWNERS not exist, load sig-info.yaml
		f, err := bot.cli.GetPathContent("wanghao75", "community",
			fmt.Sprintf("sig/%s/sig-info.yaml", sigName), "master")
		if err != nil {
			return r, nil, err
		}

		maintainers, committers := decodeSigInfoFile(*f.Content)
		fmt.Println(maintainers, committers)
		return maintainers.UnsortedList(), committers.UnsortedList(), nil
	}

	return r, nil, nil
}

func (bot *robot) createLabelIfNeed(org, repo, label string) error {
	repoLabels, err := bot.cli.GetRepoLabels(org, repo)
	if err != nil {
		return err
	}

	for _, v := range repoLabels {
		if v == label {
			return nil
		}
	}

	return bot.cli.CreateRepoLabel(org, repo, label)
}
