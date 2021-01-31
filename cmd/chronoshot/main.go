package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

func main() {

        logName := ""
        if len(os.Args) > 1 {
                logName = os.Args[1]
        } else {
		logName = "cloudtee"
                doPost(logName, []string{"Log name not specified in cloudtee"})
	}

	postTickChan := time.NewTicker(time.Second * 10).C
	stdInChan := make(chan string, 300)
	stdInEOFReached := false

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			stdInChan <- line
			fmt.Println(line)
		}
		// Check for errors during `Scan`. End of file is
		// expected and not reported by `Scan` as an error.
		if err := scanner.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			doPost(logName, []string{err.Error()})
			os.Exit(1)
		}
		stdInEOFReached = true
	}()

	for {
		select {
		case <-postTickChan:
			lines := make([]string, 0, 4096)
			chanEmpty := false
			for {
				select {
				case line := <-stdInChan:
					lines = append(lines, line)
				default:
					chanEmpty = true
				}
				if chanEmpty {
					if len(lines) > 0 {
						doPost(logName, lines)
					}
					if stdInEOFReached {
						doPost(logName, []string{"EOF"})
						os.Exit(0)
					}
					break
				}
			}
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func doPost(logName string, lines []string) {
	url := "https://know-show.azurewebsites.net/api/Log"

	var jsonStr []byte
	if len(lines) == 1 {
		jsonStr = []byte(`{"name":"` + logName + `","message":"` + lines[0] + `"}`)
	} else {
		jsonLines, _ := json.Marshal(lines)
		jsonStr = []byte(`{"name":"` + logName + `","messages":` + string(jsonLines) + `}`)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Println("response Status:", resp.Status)
	fmt.Println("response Headers:", resp.Header)
	body, _ := ioutil.ReadAll(resp.Body)
	fmt.Println("response Body:", string(body))
}
