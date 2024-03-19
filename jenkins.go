package main

type buildObject struct {
	Number int    `json:"number"`
	Url    string `json:"url"`
}
type JenkinsResponse struct {
	Builds []buildObject `json:"builds"`
}

type Pull struct {
	Number int    `json:"number"`
	Author string `json:"author"`
	Commit string `json:"sha"`
	Title  string `json:"title"`
}

type Ref struct {
	Pull    []Pull `json:"pulls"`
	BaseSHA string `json:"base_sha"`
}
type JobSpec struct {
	Refs Ref `json:"refs"`
}
type Parameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type Action struct {
	Parameters []Parameter `json:"parameters"`
}

type JobInfo struct {
	Actions []Action `json:"actions"`
	Result  string   `json:"result"`
}
