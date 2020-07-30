package main

import (
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/gin-gonic/gin"
	"github.com/jasonlvhit/gocron"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/xeonx/timeago"
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
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	panicOnError(err)
	bodyStr := string(body)
	hasLockingDiff := strings.Contains(bodyStr, code)
	return hasLockingDiff
}

type Vehicle struct {
	gorm.Model
	Price string
	Vin string
	Code string
	Equipped bool
	URL string
	Image string
	Title string
}

func (v *Vehicle) HumanUpdatedAt() string {
	return timeago.English.Format(v.UpdatedAt)
}

func migrate(db *gorm.DB) {
	// Migrate the schema
	db.AutoMigrate(&Vehicle{})
}

func loadVehicleOption(db *gorm.DB, v Vehicle, code string) {
	var vehicle Vehicle
	err := db.First(&vehicle, "vin = ? AND code = ?", v.Vin, code).Error
	if err == nil {
		log.Printf("vin %s already exists", v.Vin)
		return
	}
	hasDiff := checkVehicleForBuildCode(v.Vin, code)
	vehicle = Vehicle{
		Price: v.Price,
		Vin: v.Vin,
		Code: code,
		Equipped: hasDiff,
		URL: v.URL,
		Title: v.Title,
		Image: v.Image,
	}
	db.Save(&vehicle)
	return
}

func scrapeForVin(url string) (string, string, string, string) {
	resp, err := http.Get(url)
	panicOnError(err)
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	panicOnError(err)

	//TODO: add image
	img := ""

	body := doc.Text()

	re := regexp.MustCompile("(?P<vin>WP1[0-9A-z-]{14})")
	vin := re.Find([]byte(body))
	title := strings.Split(body, "\n")[0]
	price := doc.Find(".first-price").First().Text()
	price = "$" + strings.ReplaceAll(price, "MSRP", "")
	return string(vin), title, img, price
}

func scrapeForAutoTraderCars(searchUrl string) []string {
	u, err := url.Parse(searchUrl)
	panicOnError(err)
	re := regexp.MustCompile("\\bhref=\"(/cars-for-sale/vehicledetails[^\"]*)")
	resp, err := http.Get(searchUrl)
	panicOnError(err)
	defer resp.Body.Close()

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
			vin, title, img, price := scrapeForVin(url)
			vehicle = Vehicle{
				Price: price,
				Vin: vin,
				Title: title,
				Image: img,
				URL: url,
			}
			loadVehicleOption(db, vehicle, LOCKINGDIFF)
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
		db.Where("equipped = ? AND code = ?", true, "1Y1").Order("updated_at desc").Find(&vehicles)
		var lastUpdate string
		if len(vehicles) > 0 {
			lastUpdate = timeago.English.Format(vehicles[0].UpdatedAt)
		} else {
			lastUpdate = "never"
		}
		c.HTML(http.StatusOK, "index.html", gin.H{"vehicles": vehicles, "lastUpdated": lastUpdate})
	})
	r.GET("/scrape", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "started"})
		go scrapeTask(db)
	})
	r.Run(":8080")
}
