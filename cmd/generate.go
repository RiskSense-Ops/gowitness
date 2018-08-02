package cmd

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"sort"
	"path/filepath"
	"strings"
	"fmt"
	"text/template"

	gwtmpl "github.com/RiskSense-Ops/gowitness/template"
	log "github.com/sirupsen/logrus"

	"github.com/RiskSense-Ops/gowitness/storage"
	"github.com/spf13/cobra"
	"github.com/tidwall/buntdb"
)

// generateCmd represents the generate command
var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate an HTML report from a database file",
	Long: `
Generate an HTML report of the screenshot information found in a gowitness.db file

For example:

$ gowitness generate`,
	Run: func(cmd *cobra.Command, args []string) {

		// Populate a variable with the data the template will
		// want to parse
		var screenshotEntries []storage.HTTResponse
		var errorsIgnored = 0
		err := db.Db.View(func(tx *buntdb.Tx) error {

			tx.Ascend("", func(key, value string) bool {

				data := storage.HTTResponse{}
				log.WithField("url", value).Debug("Generating screenshot entry for"+value)
				if err := json.Unmarshal([]byte(value), &data); err != nil {
					log.Fatal(err)
				}

				// check if the screenshot path exists. if not, slide in
				// a placeholder image
				if _, err := os.Stat(data.ScreenshotFile); os.IsNotExist(err) {

					log.WithField("screenshot-file", data.ScreenshotFile).
						Debug("Adding placeholder for missing screenshot")
					data.ScreenshotFile = gwtmpl.PlaceHolderImage
				}

				log.WithField("url", data.FinalURL).Debug("Generating screenshot entry")
				if includeErrors {
					screenshotEntries = append(screenshotEntries, data)
				} else if data.ResponseCode >= 200 && data.ResponseCode < 300 {
					screenshotEntries = append(screenshotEntries, data)
				} else {
					errorsIgnored += 1
				}
				return true
			})

			return nil
		})

		// sort entries by page title
		sort.Slice(screenshotEntries, func(i,j int) bool {
			return strings.ToLower(screenshotEntries[i].PageTitle) < strings.ToLower(screenshotEntries[j].PageTitle);
		})

		// sort untitled pages by Server header. untitled pages are at the beginning of screenshotEntries
		// so we find the index of the last titled screen to define our server header sort range
		var lastSortIndex = 0
		for i, screen := range screenshotEntries {
			if screen.PageTitle != "" {
				lastSortIndex = i
				break
			}
		}
		sort.Slice(screenshotEntries[:lastSortIndex], func(i,j int) bool {
			var server_i = ""
			var server_j = ""
			for _, h := range screenshotEntries[i].Headers {
				if strings.ToLower(h.Key) == "server" { server_i = strings.ToLower(h.Value) }
			}
			for _, h := range screenshotEntries[j].Headers {
				if strings.ToLower(h.Key) == "server" { server_j = strings.ToLower(h.Value) }
			}
			return server_i < server_j
		})

		if err != nil {
			log.Fatal(err)
		}

		if len(screenshotEntries) <= 0 {
			log.WithField("count", len(screenshotEntries)).Error("No screenshot entries exist to create a report")
			return
		}

		// Prepare and render the template
		type TemplateData struct {
			ScreenShots []storage.HTTResponse
			PageIndex string
			PageCount int
			PageNext string
			PagePrev string
			PageNumber int
			ErrorsIgnored int
		}
		templateData := TemplateData{ScreenShots: screenshotEntries}

		tmplPage, err := template.New("report-page").Parse(gwtmpl.HTMLContent)
		if err != nil {
			log.WithField("err", err).Fatal("Failed to parse template")
		}

		var pageno = 0
		var pageIndex bytes.Buffer
		for i := 0; i < len(screenshotEntries); i += pageSize {
			var pageFile = fmt.Sprintf("page-%v.html",  pageno)
			pageIndex.WriteString(fmt.Sprintf("&#8226;<a class=\"page-number\" href=\"%v\">%v</a>", pageFile, pageno))
			pageno += 1
		}

		pageCount := pageno
		pageno = 0
		for i, screen := range screenshotEntries {
			if screen.ScreenshotFile != gwtmpl.PlaceHolderImage {
				screenshotEntries[i].ScreenshotFile = filepath.Base(screen.ScreenshotFile)
			}
			var headers []storage.HTTPHeader
			for _, header := range screenshotEntries[i].Headers {
				if strings.ToLower(header.Key) == "server" {
					headers = append(headers, header)
				}
			}
			screenshotEntries[i].Headers = headers
		}
		//os.MkdirAll(reportDir, 0750);
		reportDir = "."
		for i := 0; i < len(screenshotEntries); i += pageSize {
			var page bytes.Buffer
			var end = len(screenshotEntries) - i
			if pageSize < end { end = pageSize }
			var prev = fmt.Sprintf("<a id=\"prev-page\" href=\"page-%v.html\">Prev</a>", (pageno + pageCount - 1) % pageCount)
			var next = fmt.Sprintf("&#8226;<a id=\"next-page\" href=\"page-%v.html\">Next</a>", (pageno + 1) % pageCount)
			templateData = TemplateData{
				ScreenShots: screenshotEntries[i:i+end],
				PageIndex: pageIndex.String(),
				PageCount: len(screenshotEntries),
				PageNext: next,
				PagePrev: prev,
				PageNumber: pageno,
				ErrorsIgnored: errorsIgnored,
			}
			tmplPage.Execute(&page, templateData)
			var pageFile = fmt.Sprintf("%v/page-%v.html", reportDir, pageno)
			ioutil.WriteFile(pageFile, []byte(page.String()), 0640)
			pageno += 1
		}

		log.WithField("report-file", "page-0.html").Info("Report generated")
	},
}

func init() {
	RootCmd.AddCommand(generateCmd)

	//generateCmd.Flags().StringVarP(&reportDir, "report-dir", "n", "gowitnessReport", "Destination report directory")
	generateCmd.Flags().IntVarP(&pageSize, "page-size", "p", 40, "Results Per Page")
	generateCmd.Flags().BoolVarP(&includeErrors, "include-errors", "i", false, "Include non-200 responses")
}
