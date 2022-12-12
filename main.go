package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	epub_name     *string
	output_fn     *string
	deepl_api_key *string
	source_lang   *string
	dst_lang      *string
)

func init() {
	epub_name = flag.String("i", "", "specify input epub name")
	output_fn = flag.String("o", "", "specify onput epub name")
	deepl_api_key = flag.String("k", "", "Deepl api key")
	source_lang = flag.String("s", "EN", "source language")
	dst_lang = flag.String("d", "ZH", "dst language")
}

func unzip_epub(fn *string) string {
	uuidWithHyphen := uuid.New()
	log.Println("temp dir:", uuidWithHyphen)
	if err := os.Mkdir(uuidWithHyphen.String(), os.ModePerm); err != nil {
		log.Fatal(err)
	}
	dst := uuidWithHyphen.String()
	archive, err := zip.OpenReader(*fn)
	if err != nil {
		panic(err)
	}
	for _, f := range archive.File {
		filePath := filepath.Join(dst, f.Name)
		//log.Println("unzipping file ", filePath)

		if !strings.HasPrefix(filePath, filepath.Clean(dst)+string(os.PathSeparator)) {
			log.Fatal("invalid file path")
		}
		if f.FileInfo().IsDir() {
			log.Println("creating directory...")
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			panic(err)
		}

		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			panic(err)
		}

		fileInArchive, err := f.Open()
		if err != nil {
			panic(err)
		}

		if _, err := io.Copy(dstFile, fileInArchive); err != nil {
			panic(err)
		}

		dstFile.Close()
		fileInArchive.Close()
	}

	defer archive.Close()

	return uuidWithHyphen.String()
}

func repack_epub(folder_name string, repack_name string) {
	fsys := os.DirFS(folder_name)
	w, err := os.Create(repack_name)
	if err != nil {
		panic(err)
	}
	defer w.Close()
	zw := zip.NewWriter(w)
	defer zw.Close()
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		//log.Println("Zip:", p)
		zf, _ := zw.Create(p)
		f, _ := fsys.Open(p)
		defer f.Close()
		_, _ = io.Copy(zf, f)
		return nil
	})
}

func mustOpen(f string) *os.File {
	r, err := os.Open(f)
	if err != nil {
		pwd, _ := os.Getwd()
		fmt.Println("PWD: ", pwd)
		panic(err)
	}
	return r
}

func createMultipartFormData(fieldName, fileName string,
	source_lang string, target_lang string) (bytes.Buffer, *multipart.Writer) {
	var b bytes.Buffer
	var err error
	w := multipart.NewWriter(&b)
	var fw io.Writer
	file := mustOpen(fileName)

	fw, err = w.CreateFormField("source_lang")
	if err != nil {
		log.Println("Error: ", err)
	}
	_, err = io.Copy(fw, strings.NewReader(source_lang))
	if err != nil {
		log.Println("Error: ", err)
	}

	fw, err = w.CreateFormField("target_lang")
	if err != nil {
		log.Println("Error: ", err)
	}
	_, err = io.Copy(fw, strings.NewReader(target_lang))
	if err != nil {
		log.Println("Error: ", err)
	}

	if fw, err = w.CreateFormFile(fieldName, file.Name()); err != nil {
		log.Println("Error: ", err)
	}
	if _, err = io.Copy(fw, file); err != nil {
		log.Println("Error: ", err)
	}
	w.Close()
	return b, w
}

func dup_xhtml_to_html(fn string) string {
	ext := filepath.Ext(fn)
	log.Println("File extension:", ext)

	if ext == ".html" {
		return fn
	}

	//new_html, _ := filepath.Abs(fn)
	new_html := fn[0:len(fn)-len(ext)] + ".html"
	log.Println("html:", new_html)

	input, err := ioutil.ReadFile(fn)
	if err != nil {
		fmt.Println(err)
		return ""
	}

	err = ioutil.WriteFile(new_html, input, 0644)
	if err != nil {
		fmt.Println(err)
		return ""
	}
	return new_html
}

func check_document_ready(response []byte, auth_key string) (bool, string, string) {
	var result map[string]interface{}
	err := json.Unmarshal(response, &result)
	if err != nil {
		log.Println(err)
		return false, "", ""
	}
	if len(result["document_id"].(string)) > 0 {
		log.Println("id:", result["document_id"].(string))
	}
	if len(result["document_key"].(string)) > 0 {
		log.Println("key:", result["document_key"].(string))
	}
	url := "https://api.deepl.com/v2/document/" + result["document_id"].(string)

	b := "document_key=" + result["document_key"].(string)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(b)))
	if err != nil {
		return false, "", ""
	}

	req.Header.Set("Authorization", fmt.Sprintf("DeepL-Auth-Key %s", auth_key))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	repeat := 100
	for repeat > 0 {
		status_response, error := client.Do(req)

		if err != nil {
			panic(error)
		}
		defer status_response.Body.Close()

		log.Println("response Status:", status_response.Status)
		//log.Println("response Headers:", response.Header)
		body, _ := ioutil.ReadAll(status_response.Body)
		log.Println("response Body:", string(body))
		if status_response.StatusCode == 200 {
			var trans_status map[string]interface{}
			err = json.Unmarshal(body, &trans_status)
			if err == nil {
				if len(trans_status["status"].(string)) > 0 {
					log.Println("id:", trans_status["status"].(string))
					if trans_status["status"].(string) == "done" {
						return true, result["document_id"].(string), result["document_key"].(string)
					}
				}
			}
		}
		time.Sleep(5 * time.Second)
		repeat--
	}
	return false, "", ""
}

func download_trans_result(auth_key string, id string, key string) (bool, string) {
	url := "https://api.deepl.com/v2/document/" + id + "/result"
	b := "document_key=" + key

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(b)))
	if err != nil {
		return false, ""
	}
	req.Header.Set("Authorization", fmt.Sprintf("DeepL-Auth-Key %s", auth_key))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{}
	status_response, err := client.Do(req)

	if err != nil {
		return false, ""
	}
	defer status_response.Body.Close()
	log.Println("response Status:", status_response.Status)
	if status_response.StatusCode == 200 {
		body, _ := ioutil.ReadAll(status_response.Body)
		new_file := uuid.New().String()
		err = os.WriteFile(new_file, body, 0644)
		if err == nil {
			return true, new_file
		}
	}
	return false, ""
}

func Translate_xhtml(fn string, auth_key string,
	source_lang string, target_lang string) bool {
	ret := false
	url := "https://api.deepl.com/v2/document"
	temp_html := dup_xhtml_to_html(fn)
	b, w := createMultipartFormData("file", temp_html, source_lang, target_lang)

	req, err := http.NewRequest("POST", url, &b)
	if err != nil {
		return ret
	}
	// Don't forget to set the content type, this will contain the boundary.
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", fmt.Sprintf("DeepL-Auth-Key %s", auth_key))

	client := &http.Client{}
	response, error := client.Do(req)
	if err != nil {
		panic(error)
	}
	defer response.Body.Close()

	log.Println("response Status:", response.Status)
	//log.Println("response Headers:", response.Header)
	body, _ := ioutil.ReadAll(response.Body)
	log.Println("response Body:", string(body))

	doc_ready, doc_id, doc_key := check_document_ready(body, auth_key)
	log.Println("doc_ready:", doc_ready)
	if doc_ready {
		doc_ready, new_file := download_trans_result(auth_key, doc_id, doc_key)
		if doc_ready {
			e := os.Remove(fn)
			if e != nil {
				log.Fatal(e)
			}
			e = os.Rename(new_file, fn)
			if e != nil {
				log.Fatal(e)
			}
			if temp_html != fn {
				e = os.Remove(temp_html)
				if e != nil {
					log.Fatal(e)
				}
			}
			ret = true
		}

	}

	return ret
}

func main() {
	flag.Parse()
	if len(*epub_name) == 0 || len(*output_fn) == 0 || len(*deepl_api_key) == 0 {
		flag.Usage()
		os.Exit(0)
	}
	log.Println("input name:", *epub_name)
	log.Println("output name:", *output_fn)

	working_folder := unzip_epub(epub_name)
	_ = filepath.Walk(working_folder,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			fmt.Println(path, info.Size())
			ext := filepath.Ext(path)
			log.Println("Ext:", ext)
			if ext == ".xhtml" || ext == ".html" {
				Translate_xhtml(path, *deepl_api_key, *source_lang, *dst_lang)
			}
			return nil
		})

	repack_epub(working_folder, *output_fn)
}
