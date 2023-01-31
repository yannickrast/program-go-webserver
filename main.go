/*
Das Package main dient zur Erzeugung eines Webservers auf
html/template-Basis. Die darzustellenden Rohdaten werden
dabei bis auf einige Ausnahmen (größere Binärdateien)
in einer mongodb-Datenbank abgespeichert.

Autor: Yannick Rast, m29264
Datum: 31.01.2023
Kurs: BFO - Webprogrammierung 2022/2023
*/
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
	Page         Page  `bson:"page,omitempty"`         // Darzustellende Page
	IndexLink    Link  `bson:"indexLink,omitempty"`    // Indexreferenz
	MainLinks    Links `bson:"mainLinks,omitempty"`    // Mainnavigationreferenzen
	FooterLinks  Links `bson:"footerLinks,omitempty"`  // Footernavigationreferenzen
	ArticleLinks Links `bson:"articleLinks,omitempty"` // Articlereferenzen
}

type Page struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`          // Page-ObjectID
	Type        string             `bson:"type,omitempty"`         // Page-Typ
	Tag         string             `bson:"tag,omitempty"`          // Page-Tag
	Title       string             `bson:"title,omitempty"`        // Page-Titel
	Description string             `bson:"description,omitempty"`  // Page-Inhaltsbeschreibung
	Content     template.HTML      `bson:"content,omitempty"`      // Page-Inhalt
	CustomCSS   string             `bson:"customCSS,omitempty"`    // spezifische CSS-Datei
	CustomJS    string             `bson:"customScript,omitempty"` // spezifische JS-Datei
	Images      []string           `bson:"images,omitempty"`       // Bilder der Page
	Video       string             `bson:"video,omitempty"`        // YouTube Video der Page
}

type Pages []Page // Page-Array

type Link struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`        // Link-ObjectID
	Type       string             `bson:"type,omitempty"`       // Link-Typ
	Tag        string             `bson:"tag,omitempty"`        // Link-Tag
	Title      string             `bson:"title,omitempty"`      // Link-Titel
	URL        string             `bson:"url,omitempty"`        // URL
	CoverImage string             `bson:"coverImage,omitempty"` // Bildcover (bei Artikel)
}

type Links []Link // Link-Array

type ObjID struct {
	Value primitive.ObjectID `bson:"_id" json:"_id"` // ObjectID
}

func (oid *ObjID) Hex() string {
	return oid.Value.Hex() // erzeugte ObjectID
}

var (
	zipFile = "files.zip" // Rohdaten-Zip

	flsDir = flag.String("fls", "./files", "Files -Dir.")         // Dateiverzeichnis
	stcDir = flag.String("stc", "./static", "Static -Dir.")       // Static-Verzeichnis
	tmpDir = flag.String("tmp", "./templates", "Template -Dir.")  // Template-Verzeichnis
	tprDir = flag.String("tpr", "./temporary", "Temporary -Dir.") // temporäres Verzeichnis

	indexTemplate = "index.templ.html"     // Index-Template-Bezeichnung
	pageTemplate  = "page.templ.html"      // Page-Template-Bezeichnung
	slideTemplate = "slideshow.templ.html" // Slide-Show-Template-Bezeichnung
	videoTemplate = "video.templ.html"     // Video-Template-Bezeichnung
	baseTemplate  = "base.templ.html"      // Index-Template-Bezeichnung

	pageCollection *mongo.Collection // mongodb-Collection mit Pages
	linkCollection *mongo.Collection // mongodb-Collection mit Links

	indexTag = "portfolio" // Tag der Index-Page
)

// main-func zur Ausführung des Webservers
func main() {

	// Kontext
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verbindungsaufbau zur Datenbank
	opt := options.Client().ApplyURI("mongodb://root:rootpassword@mongodb_container:27017")
	client, err := mongo.Connect(ctx, opt)
	if err != nil {
		log.Fatalln(err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		log.Fatalln(err)
	}

	// Initialisierung der Collections
	pageCollection = client.Database("mongodb").Collection("pages")
	linkCollection = client.Database("mongodb").Collection("links")

	initMongo(ctx) // Datenbankinitialisierung

	createTempData() // extrahiert die Rohdaten in ein temporäres Verzeichnis

	flag.Parse() // übergibt Flags

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

// initMongo dient zur Initialisierung der Collections in der DB
func initMongo(ctx context.Context) {

	// liest Datenbank-Daten aus JSON-Datei
	pages, err := readInData("data/pages.json")

	if err != nil {
		log.Fatalln("Daten konnten nicht eingelesen werden: ", err)
	}

	// ermittelt für jede Page den jeweiligen Link und fügt beide der Datenbank hinzu
	for _, pageEntry := range pages {

		pageEntry.Tag = convertToTag(pageEntry.Title) // konvertiert den Page-Titel zum Tag

		// Link-Daten
		var linkEntry Link
		linkEntry.Type = pageEntry.Type
		linkEntry.Title = pageEntry.Title
		linkEntry.Tag = pageEntry.Tag

		// übergibt erstes Bild der Page als Coverimage an den jeweiligen Link
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

// makePageHandler dient zur Rückgabe einer Funktion zur
// Verwaltung der Übergabedaten bei Aufruf einer Page
func makePageHandler(pageType string) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		var pageTag string           // Page-Tag
		templateName := pageTemplate // Templatebezeichnung, Standard: page.template.html

		// prüft auf Page-Typ und ersetzt die Templatebezeichnung beim Fall einer Index-Page
		switch pageType {
		case "index":
			pageTag = indexTag
			templateName = indexTemplate
		case "main":
			pageTag = r.URL.Path[len("/main/"):]
		case "footer":
			pageTag = r.URL.Path[len("/footer/"):]
		case "article":
			pageTag = r.URL.Path[len("/article/"):]
		}

		var data TemplateData // Übergabedaten

		// holt page aus DB
		page, err := loadPage(pageType, pageTag)
		if err != nil {
			log.Println(err)
		}

		// holt links aus DB
		indexLink, err := loadIndexLink()
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

		// initialisiert Übergabedaten
		data.Page = page
		data.IndexLink = indexLink
		data.MainLinks = mainLinks
		data.FooterLinks = footerLinks
		data.ArticleLinks = nil

		// holt im Falle der Index-Page die article-links aus der DB
		if pageType == "index" {
			articles, err := loadLinks("article")
			if err != nil {
				log.Println(err)
			}
			data.ArticleLinks = articles
		}

		// prüft Page darauf, ob es sich um eine Slide-Show-/Video-Page handelt
		if len(page.Images) > 1 {
			templateName = slideTemplate
		} else if page.Video != "" {
			templateName = videoTemplate
		}

		log.Println("Generiere Seite: ", page.Title) // log mit Page-Titel der generierten Seite

		err = renderPage(w, data, templateName) // Ausführung des Rendervorgangs der Page
		if err != nil {
			log.Println(err)
		}
	}
}

// renderPage dient zum Rendern einer Page mitsamt der übergebenen Daten
func renderPage(w io.Writer, data interface{}, content string) error {

	// fügt Templates zusammen
	temp, err := template.ParseFiles(
		filepath.Join(*tmpDir, baseTemplate),
		filepath.Join(*tmpDir, content),
	)
	if err != nil {
		return fmt.Errorf("renderPage.Parsefiles: %w", err)
	}
	err = temp.ExecuteTemplate(w, "base", data) // Template wird ausgeführt
	if err != nil {
		return fmt.Errorf("renderPage.ExecuteTemplate: %w", err)
	}

	return nil
}

// loadPage dient zur Entnahme von Pages aus der DB
func loadPage(pageType string, pageTag string) (Page, error) {

	// Kontext
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var page Page // Seite

	// READ INDEX PAGE
	filter := bson.M{"type": pageType, "tag": pageTag} // Filter

	var resPage bson.M // entnommene Page
	pageCollection.FindOne(ctx, filter).Decode(&resPage)

	// "marshalt" die Page als bson.D, und "unmarshalt"
	// diese anschließend zu einem Byte-Array
	bsonBytes, err := bson.Marshal(resPage)
	if err != nil {
		log.Printf("could not convert to bson: %v", err)
	}
	bson.Unmarshal(bsonBytes, &page)

	return page, err // Page
}

// loadIndexLink dient zur Entnahme des Index-Links aus der DB
func loadIndexLink() (Link, error) {

	// Kontext
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var link Link // Link

	// READ LINK
	filter := bson.M{"type": "index"} // Filter
	var resLink bson.M                // Element
	if err := linkCollection.FindOne(ctx, filter).Decode(&resLink); err != nil {
		log.Printf("could not read form db: %v", err)
	}

	// "marshalt" den Link als bson.D, und "unmarshalt"
	// diesen anschließend zu einem Byte-Array
	bsonBytes, err := bson.Marshal(resLink)
	if err != nil {
		log.Printf("could not convert to bson: %v", err)
	}
	bson.Unmarshal(bsonBytes, &link)

	return link, nil // Index-Link
}

// loadLinks dient zur Entnahme der Links eines bestimmten Typs aus der DB
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
	var result []bson.M // alle gefilterten Elemente
	if err = cursor.All(ctx, &result); err != nil {
		log.Printf("could not convert from bson: %v", err)
	}

	// Für jeden Link: "marshalt" den Link als bson.D,
	// und "unmarshalt" diesen anschließend zu einem Byte-Array
	for _, resLink := range result {

		var link Link // Link

		bsonBytes, err := bson.Marshal(resLink)
		if err != nil {
			log.Printf("could not convert to bson: %v", err)
		}
		bson.Unmarshal(bsonBytes, &link)

		links = append(links, link) // fügt Link dem Link-Array hinzu
	}

	return links, nil // Rückgabe der entnommenen Links
}

// Interface für ReadCloser
type zipCloser interface {
	Close() error
}

// liest die Page-Daten aus der JSON-Datei aus der Zip
func readInData(fileName string) (Pages, error) {

	var data Pages // Pages

	zipPath := *flsDir + "/" + zipFile // Dateipfad der Zip

	// öffnet Zip-Datei und erzeugt einen ReadCloser
	readCloser, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Fatal(err)
	}
	defer readCloser.Close()

	// liest aus spezifischer Datei
	fsFile, err := readCloser.Open(fileName)
	content, _ := ioutil.ReadAll(fsFile)
	if err != nil {
		return nil, err
	}

	// "unmarshalt" eingelesenen Inhalt zu einem byte-array
	json.Unmarshal([]byte(content), &data)

	return data, nil // Rückgabe des eingelesenen Inhalts
}

// createTempData dient zur Extrahierung und temporären
// Erzeugung der Website-Rohdaten aus der Zip
func createTempData() {

	tempDir := *tprDir + "/" // Verzeichnis für temporäre Dateien

	zipPath := *flsDir + "/" + zipFile // Zip-Pfad

	// erzeugt readCloser und öffnet die Zip-Datei
	readCloser, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Fatal(err)
	}
	defer readCloser.Close()

	// erzeugt temporären Pfad, wenn nicht vorhanden
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		err := os.Mkdir(tempDir, os.ModePerm)
		if err != nil {
			log.Print(err)
		}
	}

	// durchgeht Dateien der Zip
	for _, file := range readCloser.File {

		// prüft Datei, ob diese ein Verzeichnis ist
		if !strings.Contains(file.Name, ".") {
			if _, err := os.Stat(tempDir + file.Name); os.IsNotExist(err) {
				err := os.Mkdir(tempDir+file.Name, os.ModePerm)
				if err != nil {
					log.Print(err)
				}
			}
		} else {
			tempFile, err := os.Create(tempDir + file.Name)
			log.Print("temporäre Datei erzeugt : ", tempDir+file.Name)
			if err != nil {
				log.Print("Temporäre Datei konnte nicht erzeugt werden, Grund: ", err)
			}

			// liest Inhalt der Datei ein und schreibt diesen in die temporäre Datei
			bytes, err := tempFile.Write(readContent(file))
			if err != nil {
				log.Print(err, bytes, " Bytes wurden in die temporäre Datei geschrieben")
			}
		}
	}
}

// readContent zum Lesen des Inhalts einer Datei
func readContent(file *zip.File) []byte {

	// öffnet Zip-Datei und erzeugt einen ReadCloser
	readCloser, err := file.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer readCloser.Close()

	// speichert Inhalt in einem byte-array
	content, err := ioutil.ReadAll(readCloser)
	if err != nil {
		log.Fatal(err)
	}

	return content // Inhalt
}

// convertToTag konvertiert den Titel einer Page
// in einen Tag ohne Sonderzeichen
func convertToTag(pageTitle string) string {

	pageTitle = strings.ToLower(pageTitle)
	pageTitle = strings.Replace(pageTitle, "ä", "a", -1)
	pageTitle = strings.Replace(pageTitle, "ö", "o", -1)
	pageTitle = strings.Replace(pageTitle, "ü", "u", -1)
	pageTitle = strings.Replace(pageTitle, "ß", "ss", -1)
	pageTitle = strings.Replace(pageTitle, " ", "", -1)

	return pageTitle // konvertierter Tag
}
