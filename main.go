package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type TemplateData struct {
	Page         Page  `bson:"page,omitempty"`
	MainLinks    Links `bson:"mainLinks,omitempty"`
	FooterLinks  Links `bson:"footerLinks,omitempty"`
	ArticleLinks Links `bson:"articleLinks,omitempty"`
}

type Page struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	Type        string             `bson:"type,omitempty"`
	Tag         string             `bson:"tag,omitempty"`
	Title       string             `bson:"title,omitempty"`
	Description string             `bson:"description,omitempty"`
	Content     template.HTML      `bson:"content,omitempty"`
	CustomCSS   string             `bson:"customCSS,omitempty"`
	CustomJS    string             `bson:"customScript,omitempty"`
	Images      []string           `bson:"images,omitempty"`
}

type Pages []Page

type Link struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	Type       string             `bson:"type,omitempty"`
	Tag        string             `bson:"tag,omitempty"`
	Title      string             `bson:"title,omitempty"`
	URL        string             `bson:"url,omitempty"`
	CoverImage string             `bson:"coverImage,omitempty"`
}

type Links []Link

type ObjID struct {
	Value primitive.ObjectID `bson:"_id" json:"_id"`
}

func (oid *ObjID) Hex() string {
	return oid.Value.Hex()
}

var (
	zipFile = "files.zip"

	flsDir = flag.String("fls", "./files", "Files -Dir.")
	stcDir = flag.String("stc", "./static", "Static -Dir.")
	tmpDir = flag.String("tmp", "./templates", "Template -Dir.")
	tprDir = flag.String("tpr", "./temporary", "Temporary -Dir.")

	pageTemplate = "page.templ.html"
	baseTemplate = "base.templ.html"

	pageCollection *mongo.Collection
	linkCollection *mongo.Collection
)

func main() {

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opt := options.Client().ApplyURI("mongodb://root:rootpassword@mongodb_container:27017")
	client, err := mongo.Connect(ctx, opt)
	if err != nil {
		log.Fatalln(err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		log.Fatalln(err)
	}
	pageCollection = client.Database("mongodb").Collection("pages")
	linkCollection = client.Database("mongodb").Collection("links")

	// TEST CREATETEMP
	createTempData()

	// Datenbankinitialisierung
	initMongo(ctx)

	// enthaltene IDs aus der Datenbank
	ids, err := allIds(ctx)
	log.Println(ids)
	if err != nil {
		log.Fatalln(err)
	}

	flag.Parse()

	// Static-Fileserver
	staticFS := http.FileServer(http.Dir(*stcDir))
	http.Handle("/static/", http.StripPrefix("/static/", staticFS))

	// Temporary-Fileserver
	temporaryFS := http.FileServer(http.Dir(*tprDir))
	http.Handle("/temporary/", http.StripPrefix("/temporary/", temporaryFS))

	// Template-Page-Handler
	http.HandleFunc("/", makePageHandler("index"))
	http.HandleFunc("/main/", makePageHandler("main"))
	http.HandleFunc("/footer/", makePageHandler("footer"))
	http.HandleFunc("/article/", makePageHandler("article"))

	log.Print("Listening on :9090 ....")
	tcpErr := http.ListenAndServe(":9090", nil)
	if tcpErr != nil {
		log.Fatalln(err)
	}
}

func initMongo(ctx context.Context) {

	// liest Datenbank-Daten aus JSON-Datei
	pages, error := readInData("data/pages.json")

	if error != nil {
		log.Fatalln("Daten konnten nicht eingelesen werden!")
	}

	// ermittelt für jede Page den jeweiligen Link und fügt beide der Datenbank hinzu
	for _, pageEntry := range pages {

		pageEntry.Tag = convertToTag(pageEntry.Title)

		log.Print("tag ", pageEntry.Tag)

		// Link-Daten
		var linkEntry Link
		linkEntry.Type = pageEntry.Type
		linkEntry.Title = pageEntry.Title
		linkEntry.Tag = pageEntry.Tag

		if pageEntry.Type == "article" {
			linkEntry.CoverImage = pageEntry.Images[0]
		}

		// URL
		linkEntry.URL = "/"
		if linkEntry.Type != "index" {
			linkEntry.URL = "/" + pageEntry.Type + "/" + pageEntry.Tag
		}

		// fügt Page der Datenbank hinzu
		pageResult, err := pageCollection.InsertOne(ctx, pageEntry)
		if err != nil {
			log.Fatalf("could not insert entry %v: %v", pageEntry, err)
		}
		log.Println("Added Page: %v", pageResult)

		// fügt Link der Datenbank hinzu
		linkResult, err := linkCollection.InsertOne(ctx, linkEntry)
		if err != nil {
			log.Fatalf("could not insert entry %v: %v", linkEntry, err)
		}
		log.Println("Added Link: %v", linkResult)
	}
}

func allIds(ctx context.Context) ([]ObjID, error) {

	findOptions := options.Find().SetProjection(bson.M{"_id": 1})
	allIds, err := pageCollection.Find(ctx, bson.D{}, findOptions)
	if err != nil {
		return nil, err
	}

	var ids []ObjID
	err = allIds.All(ctx, &ids)
	if err != nil {
		return nil, err
	}
	return ids, err
}

func makePageHandler(pageType string) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		var pageTitle string

		switch pageType {
		case "index":
			pageTitle = "Einleitung"
		case "main":
			pageTitle = r.URL.Path[len("/main/"):]
		case "footer":
			pageTitle = r.URL.Path[len("/footer/"):]
		case "article":
			pageTitle = r.URL.Path[len("/article/"):]
		}

		log.Print(pageTitle)

		var data TemplateData

		page, err := loadPage(pageType, pageTitle)
		if err != nil {
			log.Println(err)
		}

		mainLinks, err := loadLinks("main")
		if err != nil {
			log.Println(err)
		}
		footerLinks, err := loadLinks("footer")
		if err != nil {
			log.Println(err)
		}

		data.Page = page
		data.MainLinks = mainLinks
		data.FooterLinks = footerLinks
		data.ArticleLinks = nil

		if pageTitle == "portfolio" {
			articles, err := loadLinks("article")
			if err != nil {
				log.Println(err)
			}
			data.ArticleLinks = articles
		}

		log.Print(data.ArticleLinks)

		err = renderPage(w, data, pageTemplate)
		if err != nil {
			log.Println(err)
		}
	}
}

func renderPage(w io.Writer, data interface{}, content string) error {
	temp, err := template.ParseFiles(
		filepath.Join(*tmpDir, baseTemplate),
		filepath.Join(*tmpDir, content),
	)
	if err != nil {
		return fmt.Errorf("renderPage.Parsefiles: %w", err)
	}
	err = temp.ExecuteTemplate(w, "base", data)
	if err != nil {
		return fmt.Errorf("renderPage.ExecuteTemplate: %w", err)
	}
	return nil
}

func loadPage(pageType string, pageTag string) (Page, error) {

	// Kontext
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var page Page // Seite

	// READ INDEX PAGE
	filter := bson.M{"type": pageType, "tag": pageTag} // Filter

	var resPage bson.M // erstes Element für Filter
	pageCollection.FindOne(ctx, filter).Decode(&resPage)
	bsonBytes, err := bson.Marshal(resPage)

	if err != nil {
		log.Printf("could not convert to bson: %v", err)
	}

	bson.Unmarshal(bsonBytes, &page)

	return page, nil
}

func loadLinks(linkType string) (Links, error) {

	// Kontext
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var links Links // Links

	// READ LINKS
	filter := bson.M{"type": linkType} // Filter

	cursor, err := linkCollection.Find(ctx, filter)
	if err != nil {
		log.Printf("could not read form db: %v", err)
	}

	var result []bson.M // Alle Elemente in Filter
	if err = cursor.All(ctx, &result); err != nil {
		log.Printf("could not convert from bson: %v", err)
	}

	for _, resLink := range result {

		var link Link

		bsonBytes, err := bson.Marshal(resLink)

		if err != nil {
			log.Printf("could not convert to bson: %v", err)
		}

		bson.Unmarshal(bsonBytes, &link)

		links = append(links, link)
	}

	return links, nil
}

type myCloser interface {
	Close() error
}

// closeFile is a helper function which streamlines closing
// with error checking on different file types.
func closeFile(f myCloser) {
	err := f.Close()
	check(err)
}

// check is a helper function which streamlines error checking
func check(e error) {
	if e != nil {
		panic(e)
	}
}

func readInData(fileName string) (Pages, error) {

	var data Pages

	zipPath := *flsDir + "/" + zipFile

	zip, err := zip.OpenReader(zipPath)
	check(err)
	defer closeFile(zip)

	fc, err := zip.Open(fileName)

	content, _ := ioutil.ReadAll(fc)

	jsonData := content

	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(jsonData), &data)

	return data, nil
}

func createTempData() {

	tempDir := "./temporary/"

	zipPath := *flsDir + "/" + zipFile

	zf, err := zip.OpenReader(zipPath)
	check(err)
	defer closeFile(zf)

	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		err := os.Mkdir(tempDir, os.ModePerm)
		if err != nil {
			log.Print(err)
		}
	}

	for _, file := range zf.File {

		// prüft Datei, ob diese ein Verzeichnis ist
		if !strings.Contains(file.Name, ".") {
			if _, err := os.Stat(tempDir + file.Name); os.IsNotExist(err) {
				err := os.Mkdir(tempDir+file.Name, os.ModePerm)
				if err != nil {
					log.Print(err)
				}
			}
		}

		if strings.Contains(file.Name, ".") {
			tempFile, err := os.Create(tempDir + file.Name)
			log.Print(tempDir + file.Name)
			if err != nil {
				log.Print("Temporäre Datei konnte nicht erzeugt werden, Grund: ", err)
			}

			bytes, err := tempFile.Write(readContent(file))
			if err != nil {
				log.Print(err, bytes, " Bytes wurden in die temporäre Datei geschrieben")
			}
		}
	}
}

func readContent(file *zip.File) []byte {
	fc, err := file.Open()
	check(err)
	defer closeFile(fc)

	content, err := ioutil.ReadAll(fc)
	check(err)

	return content
}

func convertToTag(pageTitle string) string {

	pageTitle = strings.ToLower(pageTitle)
	pageTitle = strings.Replace(pageTitle, "ä", "a", -1)
	pageTitle = strings.Replace(pageTitle, "ö", "o", -1)
	pageTitle = strings.Replace(pageTitle, "ü", "u", -1)
	pageTitle = strings.Replace(pageTitle, "ß", "ss", -1)
	pageTitle = strings.Replace(pageTitle, " ", "", -1)

	return pageTitle
}
