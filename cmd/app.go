package cmd

import (
	"io/ioutil"
	"net"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func action(clictx *cli.Context) error {
	os.MkdirAll(clictx.String("output"), os.ModePerm)
	u := "http://ya.ru"
	if clictx.NArg() > 0 {
		u = clictx.Args().Get(0)
	}
	time.Now()
	log.Printf("u = %#v\n", u)

	uParsed, err := url.Parse(u)
	if err != nil {
		log.WithError(err).Error("url.Parse")
		return errors.WithStack(err)
	}

	var mu sync.Mutex
	meta := NewJSMeta()
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	stop := false
	c := colly.NewCollector(
		colly.MaxDepth(clictx.Int("max-depth")),
		colly.Async(true),
	)
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 8,
	})
	c.WithTransport(&http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 5 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       10 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	})
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		mu.Lock()
		defer mu.Unlock()
		if stop {
			return
		}
		absoluteURL := e.Request.AbsoluteURL(e.Attr("href"))
		a, err := url.Parse(absoluteURL)
		if err != nil {
			log.WithError(err).Error("url.Parse")
			return
		}
		if !strings.HasSuffix(a.Host, uParsed.Host) {
			return
		}
		for _, v := range clictx.StringSlice("filter-word") {
			if strings.Contains(absoluteURL, v) {
				return
			}
		}
		e.Request.Visit(absoluteURL)
	})

	c.OnHTML("script", func(e *colly.HTMLElement) {
		mu.Lock()
		defer mu.Unlock()
		if stop {
			return
		}
		if e.Attr("src") != "" {
			src := e.Request.AbsoluteURL(e.Attr("src"))
			resp, err := client.Get(src)
			if err != nil {
				log.WithError(err).WithField("src", src).Error("GET")
				return
			}
			defer resp.Body.Close()

			text := ""
			if resp.StatusCode == http.StatusOK {
				bodyBytes, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					log.WithError(err).Error("ReadAll")
					return
				}
				text = string(bodyBytes)
			}

			meta.Add(clictx, e.Request.URL.String(), text, External)
			meta.Add(clictx, src, text, External)
		} else if e.Text != "" {
			meta.Add(clictx, e.Request.URL.String(), e.Text, Inline)
		}
	})

	c.OnRequest(func(r *colly.Request) {
		if stop {
			return
		}
		log.Println(r.URL)
	})

	c.OnError(func(r *colly.Response, err error) {
		if stop {
			return
		}
		log.WithError(err).WithField("url", r.Request.URL).Error("")
	})

	sig := make(chan os.Signal)
	signal.Notify(sig, os.Interrupt)

	go func() {
		<-sig
		mu.Lock()
		defer mu.Unlock()
		stop = true
		err = meta.Save(clictx)
		if err != nil {
			log.WithError(err).Error("meta.Save")
			return
		}
		syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
	}()

	c.Visit(u)
	c.Wait()

	err = meta.Save(clictx)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func Main() {
	app := &cli.App{
		Flags: []cli.Flag{
			// &cli.StringFlag{
			// 	Name:    "proxy",
			// 	EnvVars: []string{"HTTPS_PROXY"},
			// 	// Value: "", // "http://127.0.0.1:8080"
			// 	Usage: "HTTP Proxy URL",
			// 	// Destination: &opts.ChromeURL,
			// 	Value: ":9222",
			// },
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   filepath.Join(".", "output"),
				Usage:   "output path",
			},
			&cli.StringFlag{
				Name:    "meta-file",
				Aliases: []string{"meta"},
				Value:   filepath.Join(".", "meta.json"),
				Usage:   "meta.json",
			},
			&cli.IntFlag{
				Name:  "max-depth",
				Value: 4,
				Usage: "max crawler recursion depth",
			},
			&cli.StringSliceFlag{
				Name:    "filter-word",
				Aliases: []string{"fw"},
				Usage:   "filter urls by word",
			},
		},
		Action: action,
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
