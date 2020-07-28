package main

import (
	"fmt"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strings"
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

func getVehicle(db *gorm.DB, vin, code, url string) {
	var vehicle Vehicle
	err := db.First(&vehicle, "vin = ? AND code = ?", vin, code).Error
	if err == nil {
		log.Println("already exists")
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
	re := regexp.MustCompile("(?P<vin>WP1AC2[0-9A-z-]{11})")
	vin := re.Find(body)
	return string(vin)
}

func main() {
	db, err := gorm.Open("sqlite3", "test.db")
	if err != nil {
		panic("failed to connect database")
	}
	defer db.Close()
	migrate(db)

	vin := "WP1AC2A23HLA97159"
	LOCKINGDIFF := "1Y1"
	code := LOCKINGDIFF
	url := "https://www.autotrader.com/cars-for-sale/vehicledetails.xhtml?listingId=555252581&zip=94117&referrer=%2Fcars-for-sale%2Fsearchresults.xhtml%3Fzip%3D94117%26startYear%3D2011%26incremental%3Dall%26endYear%3D2018%26modelCodeList%3DCAYENNE%26makeCodeList%3DPOR%26listingTypes%3DUSED%26sortBy%3DderivedpriceDESC%26engineCodes%3D8CLDR%26firstRecord%3D0%26marketExtension%3Dinclude%26searchRadius%3D0%26isNewSearch%3Dfalse&listingTypes=USED&startYear=2011&numRecords=25&firstRecord=0&endYear=2018&modelCodeList=CAYENNE&makeCodeList=POR&searchRadius=0&makeCode1=POR&modelCode1=CAYENNE&clickType=listing"
	vin = scrapeForVin(url)
	getVehicle(db, vin, code, url)
	fmt.Println(vin)
}