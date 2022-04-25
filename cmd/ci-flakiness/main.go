package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/google/go-github/v43/github"
	"github.com/slack-go/slack"
	"github.com/xanzy/go-gitlab"
	"golang.org/x/oauth2"

	"github.com/osbuild/ci-flakiness/internal/html"
)

func stringPtr(str string) *string {
	return &str
}

func boolPtr(b bool) *bool {
	return &b
}

func getFailedJobsForGivenSHA(gl *gitlab.Client, pid, sha string) []*gitlab.Job {
	log.Printf("fetching pipelines for commit %s", sha)
	pipelines, _, err := gl.Pipelines.ListProjectPipelines(pid, &gitlab.ListProjectPipelinesOptions{
		SHA: stringPtr(sha),
	})
	if err != nil {
		panic(err)
	}

	var failedJobs []*gitlab.Job
	for _, pipeline := range pipelines {
		pagesToRead := 1
		for page := 1; page < pagesToRead+1; page++ {
			log.Printf("fetching jobs for pipeline %d", pipeline.ID)
			newJobs, resp, err := gl.Jobs.ListPipelineJobs(pid, pipeline.ID, &gitlab.ListJobsOptions{
				ListOptions: gitlab.ListOptions{
					Page:    page,
					PerPage: 100,
				},
				Scope:          nil,
				IncludeRetried: boolPtr(true),
			})
			if err != nil {
				panic(err)
			}

			if page == 1 {
				pagesToRead = resp.TotalPages
			}

			for _, j := range newJobs {
				if j.Status != "failed" {
					continue
				}

				failedJobs = append(failedJobs, j)
			}
		}
	}

	return failedJobs
}

func getFailedJobs(gh *github.Client, gl *gitlab.Client, ghOwner string, ghProject string, glPid string) []*gitlab.Job {
	var SHAs []string
	page := 1
	for {
		log.Printf("fetching page %d", page)
		newPRs, resp, err := gh.PullRequests.List(context.Background(), ghOwner, ghProject, &github.PullRequestListOptions{
			State: "closed",
			Base:  "main",
			ListOptions: github.ListOptions{
				Page:    page,
				PerPage: 100,
			},
		})
		if err != nil {
			panic(err)
		}

		for _, pr := range newPRs {
			if pr.MergedAt == nil {
				continue
			}

			if pr.MergedAt.Before(time.Now().Add(-7 * 24 * time.Hour)) {
				continue
			}

			SHAs = append(SHAs, *pr.Head.SHA, *pr.MergeCommitSHA)
		}

		if resp.NextPage == 0 {
			break
		}

		page++
	}

	var failedJobs []*gitlab.Job

	for _, sha := range SHAs {
		newFailedJobs := getFailedJobsForGivenSHA(gl, glPid, sha)
		failedJobs = append(failedJobs, newFailedJobs...)
	}
	return failedJobs
}

func groupJobs(failedJobs []*gitlab.Job) [][]*gitlab.Job {
	groupingMap := make(map[string][]*gitlab.Job)

	for _, j := range failedJobs {
		if _, exists := groupingMap[j.Name]; !exists {
			groupingMap[j.Name] = []*gitlab.Job{j}
			continue
		}

		groupingMap[j.Name] = append(groupingMap[j.Name], j)
	}

	var groupedFailedJobs [][]*gitlab.Job
	for _, j := range groupingMap {
		groupedFailedJobs = append(groupedFailedJobs, j)
	}
	return groupedFailedJobs
}

func main() {
	flagImport := flag.String("import", "", "")
	flagExport := flag.String("export", "", "")
	flag.Parse()
	webhook := os.Getenv("SLACK_WEBHOOK")
	ghToken := os.Getenv("GITHUB_TOKEN")

	if *flagImport != "" && *flagExport != "" {
		panic("doesn't make sense")
	}

	reportName := time.Now().UTC().Format(time.RFC3339)

	var failedJobs []*gitlab.Job
	if *flagImport == "" {
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: ghToken},
		)
		tc := oauth2.NewClient(ctx, ts)
		gh := github.NewClient(tc)

		gl, err := gitlab.NewClient("")
		if err != nil {
			panic(err)
		}
		ghOwner := "osbuild"
		ghProject := "osbuild-composer"
		glPid := "redhat/services/products/image-builder/ci/osbuild-composer"
		failedJobs = getFailedJobs(gh, gl, ghOwner, ghProject, glPid)

		if *flagExport != "" {
			f, err := os.OpenFile(*flagExport+"/"+reportName+".json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			err = json.NewEncoder(f).Encode(failedJobs)
			if err != nil {
				panic(err)
			}
		}
	} else {
		f, err := os.Open(*flagImport)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		err = json.NewDecoder(f).Decode(&failedJobs)
		if err != nil {
			panic(err)
		}
	}

	groupedFailedJobs := groupJobs(failedJobs)

	sort.Slice(groupedFailedJobs, func(i, j int) bool {
		return len(groupedFailedJobs[i]) > len(groupedFailedJobs[j])
	})

	html.GenerateReport("docs", reportName, groupedFailedJobs)
	html.GenerateIndex("docs")

	notify(groupedFailedJobs[:10], webhook, reportName+".html")
}

func notify(jobs [][]*gitlab.Job, webhook, link string) {
	var report string
	for _, js := range jobs {
		report += fmt.Sprintf("%3d %s\n", len(js), js[0].Name)
	}

	message := fmt.Sprintf("Happy Monday osbuilders! Here's the weekly report of the top 10 most retried CI jobs over the past week:\n```%s```\nMore info will be here in a bit: https://www.osbuild.org/ci-flakiness/%s\nSee ya next week!\n:schutzbot:", report, link)

	err := slack.PostWebhook(webhook, &slack.WebhookMessage{
		Text: message,
	})
	if err != nil {
		panic(err)
	}
}
