package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
	"html/template"
	"os"
	"sort"
	"time"
)

/*
GraphQL:

{
  viewer {
	login
    repositories(first: 100, privacy: PUBLIC, orderBy: {field: PUSHED_AT, direction: DESC}) {
      nodes {
        name
        url
        primaryLanguage {
          name
        }
        pushedAt
        isFork
        refs(refPrefix: "refs/heads/", orderBy: {field: TAG_COMMIT_DATE, direction: DESC}, first: 1) {
          edges {
            node {
              name
              target {
                ... on Commit {
                  history(first: 1) {
                    edges {
                      node {
                        commitUrl
                        author {
                          user {
                            login
                            url
                          }
                        }
                      }
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}
*/

const (
	latestRepoCnt    = 7
	enableSortByName = true
	headerFile = "./HEADER.md"
	footerFile = "./FOOTER.md"
	ciCommitID       = "actions-user"
)

type latestProjectEntry struct {
	// repo info
	RepoName string
	RepoUrl  string
	RepoLang string

	// commit info
	BranchName      string
	BranchUrl       string
	CommitID        string
	CommitUrl       string
	CommitAuthorID  string
	CommitAuthorUrl string

	// time info
	Time string
}

func fmtDuration(d time.Duration) string {
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	return fmt.Sprintf("%d hours %d minutes", h, m)
}

func fetchLatestProjects(client *githubv4.Client) []latestProjectEntry {
	variables := map[string]interface{}{
		"latestRepoCnt": githubv4.Int(latestRepoCnt),
	}
	// get latest pushed repo
	var query struct {
		Viewer struct {
			Login        string
			Repositories struct {
				Nodes []struct {
					Name            string
					Description     string
					Url             string
					PrimaryLanguage struct {
						Name string
					}
					PushedAt time.Time
					IsFork   bool
					Refs     struct {
						Edges []struct {
							Node struct {
								Name   string
								Target struct {
									Commit struct {
										History struct {
											Edges []struct {
												Node struct {
													CommitUrl      string
													AbbreviatedOid string
													Author         struct {
														User struct {
															Login string
															Url   string
														}
													}
												}
											}
										} `graphql:"history(first: 1)"`
									} `graphql:"... on Commit"`
								}
							}
						}
					} `graphql:"refs(refPrefix: \"refs/heads/\", orderBy: {field: TAG_COMMIT_DATE, direction: DESC}, first: 1)"`
				}
			} `graphql:"repositories(first: $latestRepoCnt, privacy: PUBLIC, orderBy: {field: PUSHED_AT, direction: DESC})"`
		}
	}
	err := client.Query(context.Background(), &query, variables)
	if err != nil {
		panic(err)
	}

	// parse result
	var result []latestProjectEntry
	var count int = 0
	baseTime := time.Now()
	for _, repo := range query.Viewer.Repositories.Nodes {
		if repo.Name == "seolys" || repo.Name == "TIL" {
			continue
		}
		if count == 5 {
			break
		}	 
		count = count + 1

		entry := latestProjectEntry{
			RepoName:        repo.Name,
			RepoUrl:         repo.Url,
			RepoLang:        repo.PrimaryLanguage.Name,
			BranchName:      repo.Refs.Edges[0].Node.Name,
			BranchUrl:       repo.Url + "/tree/" + repo.Refs.Edges[0].Node.Name,
			CommitUrl:       repo.Refs.Edges[0].Node.Target.Commit.History.Edges[0].Node.CommitUrl,
			CommitID:        repo.Refs.Edges[0].Node.Target.Commit.History.Edges[0].Node.AbbreviatedOid,
			CommitAuthorID:  repo.Refs.Edges[0].Node.Target.Commit.History.Edges[0].Node.Author.User.Login,
			CommitAuthorUrl: repo.Refs.Edges[0].Node.Target.Commit.History.Edges[0].Node.Author.User.Url,
		}
		durationTime := baseTime.Sub(repo.PushedAt).Round(time.Minute)
		entry.Time = fmtDuration(durationTime)
		if entry.RepoLang == "" {
			entry.RepoLang = "unknown"
		}
		result = append(result, entry)
	}
	if enableSortByName {
		sort.SliceStable(result, func(i, j int) bool {
			return result[j].CommitAuthorID == ciCommitID || (result[i].CommitAuthorID == query.Viewer.Login && result[j].CommitAuthorID != query.Viewer.Login)
		})
	}

	return result
}

var markdownTmpl = `
- [{{.RepoName}}]({{.RepoUrl}}) on branch [{{.BranchName}}]({{.BranchUrl}}) with commit [{{.CommitID}}]({{.CommitUrl}}) by [@{{.CommitAuthorID}}]({{.CommitAuthorUrl}}) {{.Time}} ago  ![](https://img.shields.io/badge/language-{{.RepoLang}}-default.svg?style=flat-square)`

var markdownTableTmpl = `| [{{.RepoName}}]({{.RepoUrl}}) | [{{.BranchName}}]({{.BranchUrl}}) |[{{.CommitID}}]({{.CommitUrl}}) | [@{{.CommitAuthorID}}]({{.CommitAuthorUrl}}) |{{.Time}} | ![](https://img.shields.io/badge/language-{{.RepoLang}}-default.svg?style=flat-square)|
`

var markdownTableHeaderTmpl = `
| repo | branch | commit | author | time since last update | language |
|:---:|:---:|:---:|:---:|:---:|:---:|
`
var markdownTableFooterTmpl = "\n\n";

func main() {
	// authenticate to github
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv( "README_TOKEN" )},
	)
	httpClient := oauth2.NewClient(context.Background(), src)
	client := githubv4.NewClient(httpClient)
	r := fetchLatestProjects(client)

	// generate template
	buf := new(bytes.Buffer)
	for _, v := range r {
		t := template.New("markdown")
		t, err := t.Parse(markdownTableTmpl)
		if err != nil {
			panic(err)
		}
		err = t.Execute(buf, v)
		if err != nil {
			panic(err)
		}
	}

	// append to README-1.md && rename to README.md
	header, _ := os.OpenFile(headerFile, os.O_WRONLY|os.O_APPEND, 0755)
	footer, _ := os.OpenFile(footerFile, os.O_CREATE|os.O_RDWR, 0755)
	defer header.Close()
	defer footer.Close()

	_, _ = header.WriteString(markdownTableHeaderTmpl)
	_, _ = header.Write(buf.Bytes())
	_, _ = header.WriteString(markdownTableFooterTmpl)

	footerStat, _ := footer.Stat() // 파일 정보 가져오기
	footer.Seek(0, os.SEEK_SET)
	var footerByte = make([]byte, footerStat.Size())
	footer.Read(footerByte)

	_, _ = header.Write(footerByte)
	_ = os.Rename(headerFile, "README.md")
}
