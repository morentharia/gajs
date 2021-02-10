package cmd

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"

	"github.com/gocolly/colly/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

const (
	Inline = iota
	External
)

type JSMeta map[string]map[string]int

func NewJSMeta() JSMeta {
	return make(JSMeta)
}

func (meta JSMeta) Add(clictx *cli.Context, url, text string, t int) error {
	h := sha1.New()
	h.Write([]byte(text))
	bs := h.Sum(nil)
	hash := fmt.Sprintf("%x", bs)
	if _, ok := meta[hash]; ok != true {
		meta[hash] = make(map[string]int, 0)

		err := ioutil.WriteFile(meta.filename(clictx, hash), []byte(text), 0644)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	meta[hash][url] = t
	return nil
}

func (meta JSMeta) filename(clictx *cli.Context, prefix string) string {
	name := fmt.Sprintf("%s.js", prefix)
	if len(name) >= 255 {
		name = name[:240] + "_dot_dot_"
	}
	return filepath.Join(
		clictx.String("output"),
		colly.SanitizeFileName(fmt.Sprintf("%s.js", prefix)),
	)
}

func (meta JSMeta) Save(clictx *cli.Context) error {
	output := make(map[string][]string)
	typeByHash := make(map[string]int)
	for hash, urls := range meta {
		for url := range urls {
			if _, ok := output[url]; ok != true {
				output[url] = make([]string, 0)
			}
			output[url] = append(output[url], hash)
			typeByHash[hash] = meta[hash][url]
		}
	}

	renameToURL := make(map[string]string)
	for u := range output {
		if len(output[u]) == 1 {
			hash := output[u][0]

			if typeByHash[hash] == External {
				renameToURL[hash] = "extrnl_" + u
				delete(output, u)
			} else if typeByHash[hash] == Inline {
				renameToURL[hash] = "inline_" + u
			}
		}
	}

	for hash, urls := range meta {
		countInline := 0
		u := ""
		for url := range urls {
			if meta[hash][url] == Inline {
				countInline++
				u = url
			}
		}
		if countInline == 1 {
			renameToURL[hash] = "inline_" + u
		}
	}

	for in, out := range renameToURL {
		err := os.Rename(meta.filename(clictx, in), meta.filename(clictx, out))
		if err != nil {
			return errors.WithStack(err)
		}
		for u := range output {
			for i := 0; i < len(output[u]); i++ {
				if output[u][i] == in {
					output[u][i] = out
				}
			}
		}
	}
	for hash := range meta {
		if _, ok := renameToURL[hash]; ok == true {
			continue
		}

		newName := "????"
		if typeByHash[hash] == External {
			newName = "extrnl_" + hash
		}
		if typeByHash[hash] == Inline {
			newName = "inline_" + hash
		}

		err := os.Rename(meta.filename(clictx, hash), meta.filename(clictx, newName))
		if err != nil {
			return errors.WithStack(err)
		}

		for u := range output {
			for i := 0; i < len(output[u]); i++ {
				if output[u][i] == hash {
					output[u][i] = newName
				}
			}
		}
	}

	for u := range output {
		for i := 0; i < len(output[u]); i++ {
			output[u][i] = meta.filename(clictx, output[u][i])
		}
		sort.Strings(output[u])
	}

	res, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return errors.WithStack(err)
	}

	log.WithField("filename", clictx.String("meta-file")).Info("Save meta")
	err = ioutil.WriteFile(clictx.String("meta-file"), res, 0644)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}
