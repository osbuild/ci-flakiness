package html

import (
	"html/template"
	"io/ioutil"
	"os"
	"strings"

	"github.com/xanzy/go-gitlab"
)

func GenerateReport(dir string, name string, jobs [][]*gitlab.Job) {
	f, err := os.OpenFile(dir+"/"+name+".html", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	t, err := template.ParseFiles("internal/html/templates/job.gohtml")
	if err != nil {
		panic(err)
	}
	err = t.Execute(f, struct {
		Name string
		Jobs [][]*gitlab.Job
	}{
		Name: name,
		Jobs: jobs,
	})
	if err != nil {
		panic(err)
	}
}

func GenerateIndex(dir string) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		panic(err)
	}

	type report struct {
		Name string
		Link template.URL
	}
	var data struct {
		Reports []report
	}

	for _, f := range files {
		if f.IsDir() || f.Name() == "index.html" || strings.HasPrefix(f.Name(), ".") {
			continue
		}

		data.Reports = append(data.Reports, report{
			Name: strings.ReplaceAll(f.Name(), ".html", ""),
			Link: template.URL("./" + f.Name()),
		})
	}

	f, err := os.OpenFile(dir+"/index.html", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	t, err := template.ParseFiles("internal/html/templates/index.gohtml")
	if err != nil {
		panic(err)
	}
	err = t.Execute(f, data)
	if err != nil {
		panic(err)
	}
}
