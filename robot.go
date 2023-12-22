package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/util/sets"
	"net/http"
	"regexp"
	"sigs.k8s.io/yaml"
	"strings"

	sdk "github.com/google/go-github/v36/github"
	"github.com/opensourceways/community-robot-lib/config"
	gc "github.com/opensourceways/community-robot-lib/githubclient"
	framework "github.com/opensourceways/community-robot-lib/robot-github-framework"
	"github.com/opensourceways/community-robot-lib/utils"
	sg "github.com/opensourceways/go-gitee/gitee"
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
	welcomeMessage3 = `
Hi ***%s***, welcome to the %s Community.
I'm the Bot here serving you. You can find the instructions on how to interact with me at **[Here](%s)**.
If you have any questions, please contact the SIG: [%s](https://gitee.com/openeuler/community/tree/master/sig/%s), and any of the maintainers.
`
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
	GetPullRequestChanges(pr gc.PRInfo) ([]*sdk.CommitFile, error)
}

type iGiteeClient interface {
	GetDirectoryTree(org, repo, sha string, recursive int32) (sg.Tree, error)
}

func newRobot(cli iClient, gic iGiteeClient) *robot {
	return &robot{
		cli: cli,
		gic: gic,
	}
}

type robot struct {
	cli iClient
	gic iGiteeClient
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
		number,
	)
}

func (bot *robot) handleIssueEvent(e *sdk.IssuesEvent, pc config.Config, log *logrus.Entry) error {
	if e.GetAction() != "opened" {
		return nil
	}

	org, repo := strings.Split(e.Repo.GetFullName(), "/")[0], e.GetRepo().GetName()
	cfg, err := bot.getConfig(pc, org, repo)
	if err != nil {
		return err
	}

	author := e.GetIssue().GetUser().GetLogin()
	number := e.GetIssue().GetNumber()
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
		0,
	)
}

func (bot *robot) handle(
	org, repo, author string,
	cfg *botConfig, log *logrus.Entry,
	addMsg, addLabel func(string) error,
	number int,
) error {
	sigName, comment, err := bot.genComment(org, repo, author, number, cfg, log)
	if err != nil {
		return err
	}

	mErr := utils.NewMultiErrors()

	if number > 0 {
		resp, err := http.Get(fmt.Sprintf("https://ipb.osinfra.cn/pulls?author=%s", author))
		if err != nil {
			mErr.AddError(err)
		}
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		type T struct {
			Total int `json:"total,omitempty"`
		}

		var t T
		err = json.Unmarshal(body, &t)
		if err != nil {
			mErr.AddError(err)
		}

		if t.Total == 0 {
			if err = bot.cli.AddPRLabel(gc.PRInfo{Org: org, Repo: repo, Number: number}, "newcomer"); err != nil {
				mErr.AddError(err)
			}
		}
	}

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

func (bot robot) genComment(org, repo, author string, number int, cfg *botConfig, log *logrus.Entry) (string, string, error) {
	sigName, err := bot.getSigOfRepo(org, repo, cfg)
	if err != nil {
		return "", "", err
	}

	if sigName == "" {
		return "", "", fmt.Errorf("cant get sig name of repo: %s/%s", org, repo)
	}

	maintainers, committers, err := bot.getMaintainers(org, repo, sigName, number, cfg, log)
	if err != nil {
		return "", "", err
	}

	if maintainers == nil && committers == nil {
		return sigName, fmt.Sprintf(
				welcomeMessage3, author, cfg.CommunityName, cfg.CommandLink, sigName, sigName),
			nil
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

func (bot *robot) getMaintainers(org, repo, sigName string, number int, config *botConfig, log *logrus.Entry) ([]string, []string, error) {
	if config.WelcomeSimpler {
		membersToContact, err := bot.findSpecialContact(org, repo, number, config, log)
		if err == nil && len(membersToContact) != 0 {
			return membersToContact.UnsortedList(), nil, nil
		}
	}

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

func (bot *robot) findSpecialContact(org, repo string, number int, cfg *botConfig, log *logrus.Entry) (sets.String, error) {
	if number == 0 {
		return nil, nil
	}

	changes, err := bot.cli.GetPullRequestChanges(gc.PRInfo{Org: org, Repo: repo, Number: number})
	if err != nil {
		log.Errorf("get pr changes failed: %v", err)
		return nil, err
	}

	filePath := cfg.FilePath
	branch := cfg.FileBranch

	content, err := bot.cli.GetPathContent(org, repo, filePath, branch)
	if err != nil {
		log.Errorf("get file %s/%s/%s failed, err: %v", org, repo, filePath, err)
		return nil, err
	}

	c, err := base64.StdEncoding.DecodeString(*content.Content)
	if err != nil {
		log.Errorf("decode string err: %v", err)
		return nil, err
	}

	var r Relation

	err = yaml.Unmarshal(c, &r)
	if err != nil {
		log.Errorf("yaml unmarshal failed: %v", err)
		return nil, err
	}

	owners := sets.NewString()
	var mo []Maintainer
	for _, c := range changes {
		for _, f := range r.Relations {
			for _, ff := range f.Path {
				if strings.Contains(*c.Filename, ff) {
					mo = append(mo, f.Owner...)
				}
				if strings.Contains(ff, "/*/") {
					reg := regexp.MustCompile(strings.Replace(ff, "/*/", "/[^\\s]+/", -1))
					if ok := reg.MatchString(*c.Filename); ok {
						mo = append(mo, f.Owner...)
					}
				}
			}
		}
	}

	for _, m := range mo {
		owners.Insert(m.GiteeID)
	}

	return owners, nil
}
