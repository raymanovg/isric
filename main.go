package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type PageParam struct {
	Name       string   `yaml:"name"`
	Url        string   `yaml:"url"`
	PageRanges []string `yaml:"pageRanges"`
}

type Config struct {
	TargetDir string      `yaml:"targetDir"`
	Pages     []PageParam `yaml:"pages"`
}

var config = Config{}

func main() {
	yfile, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	if err = yaml.Unmarshal(yfile, &config); err != nil {
		log.Fatal(err)
	}

	wg := sync.WaitGroup{}
	for _, pageParam := range config.Pages {
		wg.Add(1)
		go func(pageParam PageParam) {
			if err := handle(pageParam); err != nil {
				fmt.Printf("handling %s failed: %v \n", pageParam.Name, err)
			}
			wg.Done()
		}(pageParam)
	}
	wg.Wait()
}

func handle(params PageParam) error {
	parsedUrl, _ := url.Parse(params.Url)
	baseURL := *parsedUrl
	html, err := getHTML(baseURL)
	if err != nil {
		return fmt.Errorf("error to get html page: %v", err)
	}

	for u := range parseURLs(baseURL, html, params.PageRanges) {
		html, err := getHTML(u)
		if err != nil {
			log.Printf("error to get html page %s: %v \n", u.String(), err)
			continue
		}
		for fileUrl := range getTifUrls(u, html) {
			if _, err := download(fileUrl); err != nil {
				log.Printf("unable to download file %s: %v", fileUrl.String(), err)
			}
		}
	}
	return nil
}

func getTifUrls(pageURL url.URL, page []byte) <-chan url.URL {
	urlChan := make(chan url.URL)
	go func() {
		re := regexp.MustCompile("href=\"(.*\\.tif)\"")
		matches := re.FindAllStringSubmatch(string(page), -1)
		for _, m := range matches {
			tifURL := pageURL
			tifURL.Path = path.Join(pageURL.Path, m[1])
			urlChan <- tifURL
		}
		close(urlChan)
	}()
	return urlChan
}

func parseURLs(pageURL url.URL, pageBody []byte, pageRanges []string) <-chan url.URL {
	urlChan := make(chan url.URL)
	stringPageBody := string(pageBody)
	go func() {
		for _, tpl := range buildLinkTemplates(pageRanges) {
			regxStr := fmt.Sprintf("href=\"(%s)\\/\"", tpl)
			re := regexp.MustCompile(regxStr)
			matches := re.FindAllStringSubmatch(stringPageBody, -1)
			for _, m := range matches {
				link := pageURL
				link.Path = path.Join(pageURL.Path, m[1]) + "/"
				urlChan <- link
			}
		}
		close(urlChan)
	}()
	return urlChan
}

func getHTML(url url.URL) ([]byte, error) {
	response, err := request(url)
	if err != nil {
		return nil, fmt.Errorf("unable to request page %s: %v", url.String(), err)
	}
	defer response.Body.Close()
	return ioutil.ReadAll(response.Body)
}

func download(url url.URL) (int64, error) {
	fmt.Printf("Downlading %s \n", url.String())
	response, err := request(url)
	if err != nil {
		log.Fatalf("unable to request page %s: %s", url.String(), err)
	}
	defer response.Body.Close()

	file, err := createFile(url.Path)
	if err != nil {
		return 0, fmt.Errorf("unable to downoad file: %v", err)
	}

	defer file.Close()
	return io.Copy(file, response.Body)
}

func createFile(urlPath string) (*os.File, error) {
	parts := strings.Split(urlPath, "/")
	dir := path.Join(config.TargetDir, strings.Join(parts[len(parts)-3:len(parts)-1], "/"))
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err = os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("unable to create dir %s: %v", dir, err)
		}
	}
	return os.Create(path.Join(dir, parts[len(parts)-1]))
}

func buildLinkTemplates(ranges []string) []string {
	templates := make([]string, 0, len(ranges))
	for _, p := range ranges {
		tplBuilder := strings.Builder{}
		for _, part := range strings.Split(p, "|") {
			if tplBuilder.Len() > 0 {
				tplBuilder.WriteString("|")
			}
			tplBuilder.WriteString("tileSG-")
			tplBuilder.WriteString(part)
		}
		templates = append(templates, tplBuilder.String())
	}
	return templates
}

func request(url url.URL) (*http.Response, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	request, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("unable create request: %v", err)
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/95.0.4638.54 Safari/537.36")
	request.Header.Set("accept-language", "en-GB,en-US;q=0.9,en;q=0.8,ru;q=0.7,kk;q=0.6")
	request.Header.Set("accept-encoding", "gzip, deflate, br")
	return client.Do(request)
}
