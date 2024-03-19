/*
本工具用于找到对应 commit 区间中的每一个 PR 在 ci 上跑出 unstable test 的 构建链接
这边我们只查找最后一次 commit 后失败的构建链接（先搞个宽松简单的判断条件）
Input:

	start_commit, commit 区间开始的 commit（含本commit）
	end_commit, commit 区间结束的 commit（含本commit）, 满足 start_commit 早于 end_commit

Output:

	[PR link: unstable_ut_test_link]
*/
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/go-github/v32/github"
	"golang.org/x/oauth2"
)

const (
	owner = "pingcap"
	repo  = "tiflow"
)

func getGithubClient(githubToken *string) *github.Client {
	// github token
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: *githubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	return client
}

// 根据 commit 找到对应的 PR
func getCommitRelatedPR(client *github.Client, owner string, repo string, commit *string) (pr *github.PullRequest, succ bool) {
	opt := &github.PullRequestListOptions{
		ListOptions: github.ListOptions{PerPage: 30},
		State:       "closed",
		Base:        "master",
		Sort:        "updated",
		Direction:   "desc",
	}

	for {
		PRs, resp, err := client.PullRequests.List(context.Background(), owner, repo, opt)
		if err != nil {
			fmt.Println(err)
			return nil, false
		}

		for _, pr := range PRs {
			if *pr.MergeCommitSHA == *commit {
				return pr, true
			}
		}

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	return nil, false
}

func getPRBetweenMergedTime(client *github.Client, owner string, repo string, startTime time.Time, endTime time.Time) (prs []*github.PullRequest) {
	opt := &github.PullRequestListOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		State:       "closed",
		Base:        "master",
		Sort:        "updated",
		Direction:   "desc",
	}

	var prLists []*github.PullRequest

	for {
		PRs, resp, err := client.PullRequests.List(context.Background(), owner, repo, opt)
		if err != nil {
			fmt.Println(err)
			return nil
		}

		for _, pr := range PRs {
			fmt.Println("PR number: ", pr.GetNumber(), " which is merged at: ", pr.GetMergedAt(), " and updated at: ", pr.GetUpdatedAt())
			if pr.GetMergedAt().After(startTime) && pr.GetMergedAt().Before(endTime) {
				prLists = append(prLists, pr)
				continue
			}
			// 为了性能考虑，如果一个 PR 最后一次 updated 的时间早于 start time 一周前，就直接结束往后找
			if pr.GetUpdatedAt().Before(startTime.AddDate(0, 0, -7)) {
				return prLists
			}
		}

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	return prLists

}

func getPRLastCommit(client *github.Client, owner string, repo string, prNumber int) *string {
	opt := &github.ListOptions{PerPage: 100}

	var commit string

	for {
		commits, resp, err := client.PullRequests.ListCommits(context.Background(), owner, repo, prNumber, opt)
		if err != nil {
			fmt.Println(err)
			return nil
		}

		commit = commits[len(commits)-1].GetSHA()

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	return &commit
}

func getFailedCIURLWithCommits(commitsMap *map[string]bool, jenkinsQueryURL string) (ciLists []string) {
	verifyJenkinsResponse, err := http.Get(jenkinsQueryURL + "api/json")
	if err != nil {
		fmt.Println(err)
		return
	}

	defer verifyJenkinsResponse.Body.Close()
	verifyJenkinsBodyBytes, err := io.ReadAll(verifyJenkinsResponse.Body)
	if err != nil {
		log.Fatalf("Failed to read verify response body: %v", err)
	}

	var jenkinsResponse JenkinsResponse
	if jsonErr := json.Unmarshal(verifyJenkinsBodyBytes, &jenkinsResponse); jsonErr != nil {
		log.Fatalf("Failed to unmarshal jenkins response: %v", jsonErr)
	}

	count := 0
	for _, build := range jenkinsResponse.Builds {
		count += 1

		buildJobUrl := fmt.Sprintf(jenkinsQueryURL+"%d/api/json", build.Number)
		buildJobResp, err := http.Get(buildJobUrl)
		if err != nil {
			fmt.Println(err)
			return
		}
		buildJobRespBody, err := io.ReadAll(buildJobResp.Body)
		if err != nil {
			log.Fatalf("Failed to read build job response body: %v", err)
		}

		var jobInfo JobInfo
		if jsonErr := json.Unmarshal(buildJobRespBody, &jobInfo); jsonErr != nil {
			log.Fatalf("Failed to unmarshal build job response: %v", jsonErr)
		}

		// 只需要失败的ut link
		if jobInfo.Result != "FAILURE" {
			continue
		}

		parameterIndex := len(jobInfo.Actions[0].Parameters)
		jobSpecValue := jobInfo.Actions[0].Parameters[parameterIndex-1].Value

		var jobSpec JobSpec
		if jsonErr := json.Unmarshal([]byte(jobSpecValue), &jobSpec); jsonErr != nil {
			log.Fatalf("Failed to unmarshal job spec response: %v", jsonErr)
		}
		jobCommit := jobSpec.Refs.Pull[0].Commit

		// 判断 jobCommit 是否在 commits 中
		if _, ok := (*commitsMap)[jobCommit]; ok {
			ciLists = append(ciLists, fmt.Sprintf(jenkinsQueryURL+"%d", build.Number))
		}
	}

	return ciLists

}

func getQueryURL(testType *string) (string, bool) {
	if *testType == "ut" {
		return "https://do.pingcap.net/jenkins/job/pingcap/job/tiflow/job/ghpr_verify/", true
	} else if *testType == "kafka-test" {
		return "https://do.pingcap.net/jenkins/job/pingcap/job/tiflow/job/pull_cdc_integration_kafka_test/", true
	} else if *testType == "mysql-test" {
		return "https://do.pingcap.net/jenkins/job/pingcap/job/tiflow/job/pull_cdc_integration_test/", true
	} else if *testType == "pulsar-test" {
		return "https://do.pingcap.net/jenkins/job/pingcap/job/tiflow/job/pull_cdc_integration_pulsar_test/", true
	} else if *testType == "storage-test" {
		return "https://do.pingcap.net/jenkins/job/pingcap/job/tiflow/job/pull_cdc_integration_storage_test/", true
	} else {
		fmt.Println("[ERROR]please enter the correct test type. Including ut, kafka-test, mysql-test, pulsar-test, storage-test ")
	}
	return "", false
}

func main() {

	testType := flag.String("test_type", "ut", "please enter the test type you want to check. Including ut, kafka-test, mysql-test, pulsar-test, storage-test .default is ut,")
	startCommit := flag.String("start_commit", "", "please enter the commit you want to start with")
	endCommit := flag.String("end_commit", "", "please enter the commit you want to end with")
	githubToken := flag.String("github_token", "", "please enter your github token")
	flag.Parse()

	jenkinsQueryURL, ok := getQueryURL(testType)
	if !ok {
		return
	}

	// step 1: 获取两个 commit 对应的 PR，以及对应的 merge 时间（因为排序方式没有按照 merged 时间排序，所以需要手动比较每个 PR）
	githubClient := getGithubClient(githubToken)

	startPR, succ := getCommitRelatedPR(githubClient, owner, repo, startCommit)
	if !succ {
		fmt.Println("[ERROR] failed to get start pr with commit: ", startCommit)
	}
	mergedTimeForStartPR := startPR.GetMergedAt()

	endPR, succ := getCommitRelatedPR(githubClient, owner, repo, endCommit)
	if !succ {
		fmt.Println("[ERROR] failed to get end pr with commit: ", endCommit)
	}
	mergedTimeForEndPR := endPR.GetMergedAt()

	if mergedTimeForStartPR.After(mergedTimeForEndPR) {
		fmt.Println("[ERROR] start commit merged time is after end commit merged time")
		return
	}

	// fmt.Println("Get start PR number is ", startPR.GetNumber(), " which is merged at: ", mergedTimeForStartPR, "and end PR number is ", endPR.GetNumber(), " and merged at: ", mergedTimeForEndPR)

	// step 2: 获取在对应 merge 时间区间内 merge 的 PR 信息
	prLists := getPRBetweenMergedTime(githubClient, owner, repo, mergedTimeForStartPR, mergedTimeForEndPR)
	prLists = append(prLists, startPR, endPR)

	// for _, pr := range prLists {
	// 	fmt.Println("PR number: ", pr.GetNumber(), " which is merged at: ", pr.GetMergedAt())
	// }

	// step 3: 获取每个 PR 对应的最后一个 commit 的 sha,做成一个 map
	shaMap := make(map[string]bool)
	for _, pr := range prLists {
		shaKey := *getPRLastCommit(githubClient, owner, repo, pr.GetNumber())
		shaMap[shaKey] = true
	}

	// step 4: 获取每个 commit 对应的 ci 构建 verify 链接中失败的链接
	ciLists := getFailedCIURLWithCommits(&shaMap, jenkinsQueryURL)

	fmt.Println("======Below is the unstable ut ci link======")
	for _, ciLink := range ciLists {
		fmt.Println("ci link:", ciLink)
	}

}
