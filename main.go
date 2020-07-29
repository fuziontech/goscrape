package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jasonlvhit/gocron"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func panicOnError(err error) {
	if err != nil {
		log.Panic(err)
	}
}

func checkVehicleForBuildCode(vin string, code string) bool {
	resp, err := http.Get(fmt.Sprintf("https://vinanalytics.com/car/%s/", vin))
	panicOnError(err)
	body, err := ioutil.ReadAll(resp.Body)
	panicOnError(err)
	bodyStr := string(body)
	hasLockingDiff := strings.Contains(bodyStr, code)
	return hasLockingDiff
}

type Vehicle struct {
	gorm.Model
	Vin string
	Code string
	Equipped bool
	URL string
}

func migrate(db *gorm.DB) {
	// Migrate the schema
	db.AutoMigrate(&Vehicle{})
}

func loadVehicleOption(db *gorm.DB, vin, code, url string) {
	var vehicle Vehicle
	err := db.First(&vehicle, "vin = ? AND code = ?", vin, code).Error
	if err == nil {
		log.Printf("vin %s already exists", vin)
		return
	}
	hasDiff := checkVehicleForBuildCode(vin, code)
	vehicle = Vehicle{Vin: vin, Code: code, Equipped: hasDiff, URL: url}
	db.Save(&vehicle)
	return
}

func scrapeForVin(url string) string {
	resp, err := http.Get(url)
	panicOnError(err)
	body, err := ioutil.ReadAll(resp.Body)
	panicOnError(err)
	re := regexp.MustCompile("(?P<vin>WP1[0-9A-z-]{14})")
	vin := re.Find(body)
	return string(vin)
}

func scrapeForAutoTraderCars(searchUrl string) []string {
	u, err := url.Parse(searchUrl)
	panicOnError(err)
	re := regexp.MustCompile("\\bhref=\"(/cars-for-sale/vehicledetails[^\"]*)")
	resp, err := http.Get(searchUrl)
	panicOnError(err)
	body, err := ioutil.ReadAll(resp.Body)
	cars := re.FindAll(body, -1)
	var urls []string
	for _, car := range cars {
		link := string(car)
		link = strings.ReplaceAll(link,"href=\"", "")
		urls = append(urls, "https://" + u.Host + link)
	}
	return urls
}

func scrapeTask(db *gorm.DB) {
	LOCKINGDIFF := "1Y1"

	// numRecords=25&firstRecord=25
	firstRecordKey := "firstRecord"
	firstRecord := 0
	numRecordsKey := "numRecords"
	numRecords := 50

	baseListingsUrl := "https://www.autotrader.com/cars-for-sale/Used+Cars/8+Cylinder/Porsche/Cayenne/San+Francisco+CA-94117?makeCodeList=POR&listingTypes=USED&searchRadius=0&modelCodeList=CAYENNE&zip=94117&endYear=2018&marketExtension=include&engineCodes=8CLDR&startYear=2011&isNewSearch=true&sortBy=derivedpriceDESC"
	params := url.Values{}
	params.Add(firstRecordKey, strconv.Itoa(firstRecord))
	params.Add(numRecordsKey, strconv.Itoa(numRecords))
	listingsUrl := baseListingsUrl + "&" + params.Encode()
	urls := scrapeForAutoTraderCars(listingsUrl)

	for len(urls) > 5 {
		params := url.Values{}
		params.Add(firstRecordKey, strconv.Itoa(firstRecord))
		params.Add(numRecordsKey, strconv.Itoa(numRecords))
		listingsUrl = baseListingsUrl + "&" + params.Encode()

		urls = scrapeForAutoTraderCars(listingsUrl)
		for _, url := range urls {
			url = strings.Split(url, ";clickType")[0]

			var vehicle Vehicle
			err := db.First(&vehicle, "url = ?", url).Error
			if err == nil {
				log.Println(url)
				log.Println("vehicle has been scraped before")
				continue
			}
			time.Sleep(time.Second * 3)
			log.Println(url)
			log.Println("scraping url for vin")
			vin := scrapeForVin(url)
			loadVehicleOption(db, vin, LOCKINGDIFF, url)
		}
		firstRecord = firstRecord + 50
		log.Printf("turning to next page and starting with vehicle %d", firstRecord)
	}
	log.Printf("done with vehicle %d", firstRecord)
}

func main() {
	db, err := gorm.Open("sqlite3", "test.db")
	if err != nil {
		log.Panic("failed to connect database", err)
	}
	defer db.Close()
	migrate(db)

	gocron.Every(6).Hours().Do(scrapeTask, db)

	r := gin.Default()
	r.LoadHTMLGlob("templates/*")
	r.GET("/", func(c *gin.Context) {
		var vehicles []Vehicle
		db.Where("equipped = ? AND code = ?", true, "1Y1").Find(&vehicles)
		c.HTML(http.StatusOK, "index.html", gin.H{"vehicles": vehicles})
	})
	r.GET("/scrape", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "started"})
		scrapeTask(db)
	})
	r.Run(":8080")
}
