package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

type char struct {
	simplify    string
	traditional string
	jixiong     string
	wuxing      string
	bihua       int
}

type result struct {
	Name string
	
	Tiange string
	Renge  string
	Dige   string
	Waige  string
	Zongge string
	
	Sancai       string
	Jichuyun     string
	Chenggongyun string
	Shejiaoyun   string
	
	Rengeanshi  string
	Digeanshi   string
	Waigeanshi  string
	Zonggeanshi string
	
	Zongping string
	Score    float64
}

const (
	charSetFile = `C:\Users\35896\Desktop\laibao-naming\Char.csv`
	queryURL    = `https://www.xingming.com/dafen/`
	fixedChar   = "童"
	// fixLastChar: 固定最后一个字，否则固定第二个字
	fixLastChar = false
	// enableTotalBihuaFilter: 限制总笔画
	enableTotalBihuaFilter = true
	tatalBihuaLimit        = 40
	// enableScoreFilter: 总分过滤，只选高于分数的
	enableScoreFilter = true
	scoreLimit        = 90
)

var (
	resultJSONFile = ""
	resultCSVFile  = ""
	
	wugeReg         = regexp.MustCompile(`.*『数理』.*`)
	sancaiReg       = regexp.MustCompile(`.*您姓名的天地人三才配置.*`)
	jichuyunReg     = regexp.MustCompile(`.*<B>基础运</B>.*`)
	chenggongyunReg = regexp.MustCompile(`.*<B>成功运</B>.*`)
	shejiaoyunReg   = regexp.MustCompile(`.*<B>社交运</B>.*`)
	rengeshuliReg   = regexp.MustCompile(`.*人格\d+之数理暗示.*`)
	digeshuliReg    = regexp.MustCompile(`.*地格\d+之数理暗示.*`)
	waigeshuliReg   = regexp.MustCompile(`.*外格\d+之数理暗示.*`)
	zonggeshuliReg  = regexp.MustCompile(`.*总格\d+之数理暗示.*`)
	zongpingReg     = regexp.MustCompile(`.*根据姓名网·名字测试打.*`)
	scoreReg        = regexp.MustCompile(`>[1-9][0-9]*([\.][0-9])?<`)
	
	failedNames     = make([]string, 0)
	failedNamesLock = sync.Mutex{}
	
	scoreFiltered = atomic.Int32{}
)

func main() {
	if fixLastChar {
		resultJSONFile = fmt.Sprintf(`C:\Users\35896\Desktop\laibao-naming\result_-%v.json`, fixedChar)
		resultCSVFile = fmt.Sprintf(`C:\Users\35896\Desktop\laibao-naming\result_-%v.csv`, fixedChar)
	} else {
		resultJSONFile = fmt.Sprintf(`C:\Users\35896\Desktop\laibao-naming\result_%v-.json`, fixedChar)
		resultCSVFile = fmt.Sprintf(`C:\Users\35896\Desktop\laibao-naming\result_%v-.csv`, fixedChar)
	}
	// read charset
	chars := readCsv()
	// generate name
	names := generateName(chars)
	// names = names[:200]
	fmt.Printf("all names generated, count: %v\n", len(names))
	if len(names) == 0 {
		fmt.Println("all names unavailable. please check fixed char")
		return
	}
	// check valid name
	for _, v := range names {
		if utf8.RuneCountInString(v) != 2 {
			panic(v)
		}
	}
	
	wg := sync.WaitGroup{}
	resCh := make(chan result, 1000)
	nameCh := make(chan string, 100)
	results := make([]result, 0)
	concurrent := 100
	// collector
	go func() {
		for res := range resCh {
			results = append(results, res)
		}
	}()
	// worker
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range nameCh {
				res := querySingle(name)
				if enableScoreFilter {
					if res.Score < scoreLimit {
						scoreFiltered.Add(1)
						continue
					}
				}
				if res.Name == "" {
					continue
				}
				resCh <- res
			}
		}()
	}
	// producer
	fmt.Printf("total name: %v. start processing\n", len(names))
	for index, name := range names {
		if index%100 == 0 {
			fmt.Printf("%v/%v names have been processed\n", index, len(names))
		}
		nameCh <- name
	}
	close(nameCh)
	wg.Wait()
	fmt.Println("all names have been sent to worker")
	close(resCh)
	// marshal to json
	js, err := json.Marshal(results)
	if err != nil {
		panic(err)
	}
	// write json to file
	resJSONFile, err := os.OpenFile(resultJSONFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		panic(err)
	}
	defer resJSONFile.Close()
	_, err = resJSONFile.Write(js)
	if err != nil {
		panic(err)
	}
	
	// format to csv and write to csv
	resCSVFile, err := os.OpenFile(resultCSVFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		panic(err)
	}
	defer resCSVFile.Close()
	resCSVFile.WriteString("\xEF\xBB\xBF") // utf-8 BOM
	writer := csv.NewWriter(resCSVFile)
	defer writer.Flush()
	
	records := make([][]string, 0)
	records = append(records, []string{"Name", "Tiange", "Renge", "Dige", "Waige", "Zongge", "Sancai", "Jichuyun", "Chenggongyun", "Shejiaoyun", "Rengeanshi", "Digeanshi", "Waigeanshi", "Zonggeanshi", "Zongping", "Score"})
	for _, res := range results {
		records = append(records, []string{
			res.Name,
			res.Tiange,
			res.Renge,
			res.Dige,
			res.Waige,
			res.Zongge,
			res.Sancai,
			res.Jichuyun,
			res.Chenggongyun,
			res.Shejiaoyun,
			res.Rengeanshi,
			res.Digeanshi,
			res.Waigeanshi,
			res.Zonggeanshi,
			res.Zongping,
			fmt.Sprintf("%v", res.Score),
		})
	}
	if err := writer.WriteAll(records); err != nil {
		panic(err)
	}
	
	fmt.Println(failedNames)
	if enableScoreFilter {
		fmt.Printf("score filtered: %v/%v\n", scoreFiltered.Load(), len(names))
	}
	
	fmt.Println("all has been processed. bye")
}

func readCsv() []char {
	content, err := os.ReadFile(charSetFile)
	if err != nil {
		panic(err)
	}
	
	structContents, err := csv.NewReader(strings.NewReader(string(content))).ReadAll()
	if err != nil {
		panic(err)
	}
	
	chars := make([]char, 0)
	for index, structContent := range structContents {
		// skip title
		if index == 0 {
			continue
		}
		
		tmp := char{
			simplify:    structContent[1],
			traditional: structContent[2],
			jixiong:     structContent[7],
			wuxing:      structContent[8],
			bihua:       mustToInt(structContent[10]),
		}
		chars = append(chars, tmp)
	}
	return chars
}

// generateName will generate name like _乐
func generateName(chars []char) []string {
	// get last char bihua
	fixedCharBihua := 0
	for _, char := range chars {
		if char.simplify == fixedChar {
			fixedCharBihua = char.bihua
			break
		}
	}
	if fixedCharBihua == 0 {
		panic("last char not found in dic")
	}
	
	res := make([]string, 0)
	if fixLastChar {
		for _, char := range chars {
			// filter rule 1: Renge not in (21, 23, 26, 28, 29, 33, 34, 39)
			renge := 13 + char.bihua
			if renge == 21 || renge == 23 || renge == 26 || renge == 28 || renge == 29 || renge == 33 || renge == 34 || renge == 39 {
				continue
			}
			// filter rule 2: wuxing ne 水/金
			if char.wuxing == "水" || char.wuxing == "金" {
				continue
			}
			// filter rule 3: jixiong ne 凶
			if char.jixiong == "凶" {
				continue
			}
			// filter rule 4: dige not in (21, 23, 26, 28, 29, 33, 34, 39)
			dige := char.bihua + fixedCharBihua
			if dige == 21 || dige == 23 || dige == 26 || dige == 28 || dige == 29 || dige == 33 || dige == 34 || dige == 39 {
				continue
			}
			// filter rule 5: zongbihua limit
			if enableTotalBihuaFilter {
				zongge := 13 + dige
				if zongge > tatalBihuaLimit {
					continue
				}
			}
			
			res = append(res, fmt.Sprintf("%v%v", char.simplify, fixedChar))
		}
	} else {
		// filter rule 1: Renge not in (21, 23, 26, 28, 29, 33, 34, 39)
		renge := 13 + fixedCharBihua
		if renge == 21 || renge == 23 || renge == 26 || renge == 28 || renge == 29 || renge == 33 || renge == 34 || renge == 39 {
			return res
		}
		for _, char := range chars {
			// filter rule 2: wuxing ne 水/金
			if char.wuxing == "水" || char.wuxing == "金" {
				continue
			}
			// filter rule 3: jixiong ne 凶
			if char.jixiong == "凶" {
				continue
			}
			// filter rule 4: dige not in (21, 23, 26, 28, 29, 33, 34, 39)
			dige := char.bihua + fixedCharBihua
			if dige == 21 || dige == 23 || dige == 26 || dige == 28 || dige == 29 || dige == 33 || dige == 34 || dige == 39 {
				continue
			}
			// filter rule 5: zongbihua limit
			if enableTotalBihuaFilter {
				zongge := 13 + dige
				if zongge > tatalBihuaLimit {
					continue
				}
			}
			
			res = append(res, fmt.Sprintf("%v%v", fixedChar, char.simplify))
		}
	}
	return res
}

func querySingle(name string) result {
	param := url.Values{}
	param.Add("xs", "杨")
	param.Add("mz", name)
	param.Add("action", "test")
	resp, err := http.PostForm(queryURL, param)
	if err != nil {
		panic(err)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	// fmt.Println(string(content))
	wuge := wugeReg.FindAllString(string(content), -1)
	
	if len(wuge) != 5 {
		failedNamesLock.Lock()
		defer failedNamesLock.Unlock()
		failedNames = append(failedNames, name)
		fmt.Printf("name: %v, wuge actually returns %v, wuge: %v\n", name, len(wuge), wuge)
		return result{}
	}
	res := result{
		Name:   name,
		Tiange: formatResult(wuge[0]),
		Renge:  formatResult(wuge[1]),
		Dige:   formatResult(wuge[2]),
		Waige:  formatResult(wuge[3]),
		Zongge: formatResult(wuge[4]),
	}
	
	// extract Sancai
	sancai := sancaiReg.FindString(string(content))
	res.Sancai = formatResult(sancai)
	
	// extract Jichuyun, Chenggongyun, Shejiaoyun
	jichuyun := jichuyunReg.FindString(string(content))
	_, jichuyun, _ = strings.Cut(jichuyun, "：")
	jichuyun = strings.Replace(jichuyun, "</p>", "", -1)
	res.Jichuyun = formatResult(jichuyun)
	
	chenggongyun := chenggongyunReg.FindString(string(content))
	_, chenggongyun, _ = strings.Cut(chenggongyun, "：")
	chenggongyun = strings.Replace(chenggongyun, "</p>", "", -1)
	res.Chenggongyun = formatResult(chenggongyun)
	
	shejiaoyun := shejiaoyunReg.FindString(string(content))
	_, shejiaoyun, _ = strings.Cut(shejiaoyun, "：")
	shejiaoyun = strings.Replace(shejiaoyun, "</p>", "", -1)
	res.Shejiaoyun = formatResult(shejiaoyun)
	
	// extract shuli anshi
	rengeshuli := rengeshuliReg.FindString(string(content))
	_, rengeshuli, _ = strings.Cut(rengeshuli, "：")
	rengeshuli = strings.Replace(rengeshuli, "</p>", "", -1)
	res.Rengeanshi = formatResult(rengeshuli)
	
	digeshuli := digeshuliReg.FindString(string(content))
	_, digeshuli, _ = strings.Cut(digeshuli, "：")
	digeshuli = strings.Replace(digeshuli, "</p>", "", -1)
	res.Digeanshi = formatResult(digeshuli)
	
	waigeshuli := waigeshuliReg.FindString(string(content))
	_, waigeshuli, _ = strings.Cut(waigeshuli, "：")
	waigeshuli = strings.Replace(waigeshuli, "</p>", "", -1)
	res.Waigeanshi = formatResult(waigeshuli)
	
	zonggeshuli := zonggeshuliReg.FindString(string(content))
	_, zonggeshuli, _ = strings.Cut(zonggeshuli, "：")
	zonggeshuli = strings.Replace(zonggeshuli, "</p>", "", -1)
	res.Zonggeanshi = formatResult(zonggeshuli)
	
	// extract 总评，分数
	zongping := zongpingReg.FindString(string(content))
	res.Zongping = formatResult(zongping)
	
	scoreStr := scoreReg.FindString(zongping)
	scoreStr = strings.Replace(scoreStr, ">", "", 1)
	scoreStr = strings.Replace(scoreStr, "<", "", 1)
	res.Score = mustToFloat64(scoreStr)
	
	return res
}

// helpers

func mustToInt(s string) int {
	res, err := strconv.Atoi(s)
	if err != nil {
		panic(fmt.Sprintf("string to int on bihua panic. err is: %v, string is: %v", err, s))
	}
	return res
}

func mustToFloat64(s string) float64 {
	res, err := strconv.ParseFloat(s, 32)
	if err != nil {
		panic(fmt.Sprintf("string to float on score panic. err is: %v, string is: %v", err, s))
	}
	return res
}

func formatResult(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Replace(s, "</p><p>", "\n", -1)
	s = strings.Replace(s, "<p>", "", -1)
	s = strings.Replace(s, "</p>", "", -1)
	return s
}
