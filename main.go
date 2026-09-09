// main.go implements a web scraper for Trimill Machines product PDFs.
package main

// Import the required packages for this program.
import (
	"fmt"      // fmt provides formatted printing to the console.
	"io"       // io provides basic input/output utilities like copying streams.
	"net/http" // net/http lets us make HTTP requests to fetch web pages and files.
	"net/url"  // net/url helps parse and resolve URLs relative to a base URL.
	"os"       // os lets us interact with the filesystem, like creating folders and files.
	"path"     // path helps manipulate URL/file paths safely.
	"regexp"   // regexp lets us use regular expressions to find links in HTML.
	"strings"  // strings provides helper functions for string manipulation.
	"time"     // time lets us add delays between requests to be polite to the server.

	"golang.org/x/net/html" // html provides an HTML tokenizer/parser for extracting links.
)

// baseWebsiteURL is the root URL of the Trimill Machines website.
const baseWebsiteURL = "https://www.trimill-machines.com"

// productsPageURL is the starting page that lists top-level product categories.
const productsPageURL = "https://www.trimill-machines.com/products/"

// pdfDownloadFolderName is the local folder where downloaded PDFs will be saved.
const pdfDownloadFolderName = "PDFs"

// visitedCategoryURLs keeps track of category URLs we have already visited, to avoid repeats.
var visitedCategoryURLs = make(map[string]bool)

// visitedProductURLs keeps track of product URLs we have already visited, to avoid repeats.
var visitedProductURLs = make(map[string]bool)

// downloadedPdfFileNames keeps track of PDF filenames we have already downloaded, to avoid duplicates.
var downloadedPdfFileNames = make(map[string]bool)

// categoryURLsToVisit is a queue (slice) of category URLs that still need to be visited.
var categoryURLsToVisit = []string{}

// productURLsToVisit is a queue (slice) of product URLs that still need to be visited.
var productURLsToVisit = []string{}

// main is the entry point of the program.
func main() {
	// Create the local PDFs folder if it does not already exist, with standard permissions.
	createPdfFolderIfNotExists()

	// Fetch the raw HTML content of the main products page.
	initialPageHtmlContent, initialPageFetchError := fetchHtmlContentFromURL(productsPageURL)
	// Check if there was an error fetching the initial products page.
	if initialPageFetchError != nil {
		// Print the error and stop the program since we cannot proceed without the first page.
		fmt.Println("Error fetching products page:", initialPageFetchError)
		// Exit the program since nothing else can be done without this page.
		return
	}

	// Extract all category and product links found on the initial products page.
	discoveredCategoryLinks, discoveredProductLinks := extractCategoryAndProductLinksFromHtml(initialPageHtmlContent, productsPageURL)

	// Loop through each discovered category link from the initial page.
	for _, singleCategoryLink := range discoveredCategoryLinks {
		// Add the category link to the queue of categories still needing to be visited.
		addCategoryURLToQueueIfNew(singleCategoryLink)
	}

	// Loop through each discovered product link from the initial page.
	for _, singleProductLink := range discoveredProductLinks {
		// Add the product link to the queue of products still needing to be visited.
		addProductURLToQueueIfNew(singleProductLink)
	}

	// Continue processing categories as long as there are unvisited categories in the queue.
	for len(categoryURLsToVisit) > 0 {
		// Remove and return the first category URL from the front of the queue.
		currentCategoryURL := categoryURLsToVisit[0]
		// Reslice the queue to remove the URL we just took out.
		categoryURLsToVisit = categoryURLsToVisit[1:]

		// Mark this category URL as visited so we never process it again.
		visitedCategoryURLs[currentCategoryURL] = true

		// Print a status message showing which category is being visited.
		fmt.Println("Visiting category:", currentCategoryURL)

		// Pause briefly between requests to avoid overwhelming the server.
		time.Sleep(500 * time.Millisecond)

		// Fetch the HTML content of the current category page.
		categoryPageHtmlContent, categoryFetchError := fetchHtmlContentFromURL(currentCategoryURL)
		// Check if there was an error fetching this particular category page.
		if categoryFetchError != nil {
			// Print the error but continue with other categories rather than stopping entirely.
			fmt.Println("Error fetching category page:", currentCategoryURL, categoryFetchError)
			// Skip to the next iteration of the loop since this page failed.
			continue
		}

		// Extract any further category and product links found on this category page.
		newCategoryLinksFound, newProductLinksFound := extractCategoryAndProductLinksFromHtml(categoryPageHtmlContent, currentCategoryURL)

		// Loop through each newly found category link on this page.
		for _, newCategoryLink := range newCategoryLinksFound {
			// Add the new category link to the queue if it has not been seen before.
			addCategoryURLToQueueIfNew(newCategoryLink)
		}

		// Loop through each newly found product link on this page.
		for _, newProductLink := range newProductLinksFound {
			// Add the new product link to the queue if it has not been seen before.
			addProductURLToQueueIfNew(newProductLink)
		}
	}

	// Continue processing products as long as there are unvisited products in the queue.
	for len(productURLsToVisit) > 0 {
		// Remove and return the first product URL from the front of the queue.
		currentProductURL := productURLsToVisit[0]
		// Reslice the queue to remove the URL we just took out.
		productURLsToVisit = productURLsToVisit[1:]

		// Mark this product URL as visited so we never process it again.
		visitedProductURLs[currentProductURL] = true

		// Print a status message showing which product page is being visited.
		fmt.Println("Visiting product page:", currentProductURL)

		// Pause briefly between requests to avoid overwhelming the server.
		time.Sleep(500 * time.Millisecond)

		// Fetch the HTML content of the current product page.
		productPageHtmlContent, productFetchError := fetchHtmlContentFromURL(currentProductURL)
		// Check if there was an error fetching this particular product page.
		if productFetchError != nil {
			// Print the error but continue with other products rather than stopping entirely.
			fmt.Println("Error fetching product page:", currentProductURL, productFetchError)
			// Skip to the next iteration of the loop since this page failed.
			continue
		}

		// Extract all PDF file links found on this product page.
		pdfLinksFoundOnPage := extractPdfLinksFromHtml(productPageHtmlContent, currentProductURL)

		// Loop through each PDF link found on this product page.
		for _, singlePdfLink := range pdfLinksFoundOnPage {
			// Download the PDF file, skipping it automatically if it already exists.
			downloadPdfFileIfNotAlreadyDownloaded(singlePdfLink)
		}
	}

	// Print a final message once all categories, products, and PDFs have been processed.
	fmt.Println("Scraping complete. All discovered PDFs have been downloaded to the", pdfDownloadFolderName, "folder.")
}

// createPdfFolderIfNotExists creates the local PDFs directory if it doesn't already exist.
func createPdfFolderIfNotExists() {
	// Attempt to create the directory (and any needed parents) with read/write/execute permissions.
	folderCreationError := os.MkdirAll(pdfDownloadFolderName, 0755)
	// Check if an error occurred while creating the folder.
	if folderCreationError != nil {
		// Print the error since we cannot save PDFs without this folder.
		fmt.Println("Error creating PDF folder:", folderCreationError)
	}
}

// fetchHtmlContentFromURL performs an HTTP GET request and returns the response body as a string.
func fetchHtmlContentFromURL(targetURL string) (string, error) {
	// Create a new HTTP client with a reasonable timeout to avoid hanging forever.
	httpClientWithTimeout := &http.Client{Timeout: 20 * time.Second}
	// Perform the GET request to the target URL.
	httpResponse, httpRequestError := httpClientWithTimeout.Get(targetURL)
	// Check if the request itself failed (e.g., network error).
	if httpRequestError != nil {
		// Return an empty string and the error to the caller.
		return "", httpRequestError
	}
	// Ensure the response body is closed once this function returns, to free resources.
	defer httpResponse.Body.Close()

	// Read the entire response body into a byte slice.
	responseBodyBytes, bodyReadError := io.ReadAll(httpResponse.Body)
	// Check if reading the response body failed.
	if bodyReadError != nil {
		// Return an empty string and the error to the caller.
		return "", bodyReadError
	}

	// Convert the byte slice into a string and return it along with a nil error.
	return string(responseBodyBytes), nil
}

// extractCategoryAndProductLinksFromHtml parses HTML and returns slices of category and product URLs found.
func extractCategoryAndProductLinksFromHtml(htmlContent string, currentPageURL string) ([]string, []string) {
	// Create an empty slice to hold category URLs discovered on this page.
	discoveredCategoryURLs := []string{}
	// Create an empty slice to hold product URLs discovered on this page.
	discoveredProductURLs := []string{}

	// Create a new HTML tokenizer from the HTML content string.
	htmlTokenizer := html.NewTokenizer(strings.NewReader(htmlContent))

	// Loop indefinitely until we reach the end of the HTML document.
	for {
		// Get the next token type from the tokenizer.
		tokenType := htmlTokenizer.Next()

		// Check if we have reached the end of the document or an error occurred.
		if tokenType == html.ErrorToken {
			// Break out of the loop since there is nothing left to parse.
			break
		}

		// Get the actual token data (the parsed tag) from the tokenizer.
		currentToken := htmlTokenizer.Token()

		// Check if this token is a start tag for an anchor ("a") element, which represents a link.
		if tokenType == html.StartTagToken && currentToken.Data == "a" {
			// Loop through all attributes of this anchor tag.
			for _, singleAttribute := range currentToken.Attr {
				// Check if the attribute is the "href" attribute, which holds the link URL.
				if singleAttribute.Key == "href" {
					// Resolve the href value into an absolute URL relative to the current page.
					absoluteResolvedURL := resolveToAbsoluteURL(currentPageURL, singleAttribute.Val)

					// Check if the resolved URL is a category page URL.
					if strings.Contains(absoluteResolvedURL, "/category/") {
						// Append this URL to the slice of discovered category URLs.
						discoveredCategoryURLs = append(discoveredCategoryURLs, absoluteResolvedURL)
					} else if strings.Contains(absoluteResolvedURL, "/products/") && absoluteResolvedURL != productsPageURL {
						// Append this URL to the slice of discovered product URLs, excluding the main products page itself.
						discoveredProductURLs = append(discoveredProductURLs, absoluteResolvedURL)
					}
				}
			}
		}
	}

	// Return both slices of discovered URLs to the caller.
	return discoveredCategoryURLs, discoveredProductURLs
}

// extractPdfLinksFromHtml parses HTML and returns a slice of absolute PDF URLs found on the page.
func extractPdfLinksFromHtml(htmlContent string, currentPageURL string) []string {
	// Create an empty slice to hold PDF URLs discovered on this page.
	discoveredPdfURLs := []string{}

	// Create a new HTML tokenizer from the HTML content string.
	htmlTokenizer := html.NewTokenizer(strings.NewReader(htmlContent))

	// Loop indefinitely until we reach the end of the HTML document.
	for {
		// Get the next token type from the tokenizer.
		tokenType := htmlTokenizer.Next()

		// Check if we have reached the end of the document or an error occurred.
		if tokenType == html.ErrorToken {
			// Break out of the loop since there is nothing left to parse.
			break
		}

		// Get the actual token data (the parsed tag) from the tokenizer.
		currentToken := htmlTokenizer.Token()

		// Check if this token is a start tag for an anchor ("a") element, which represents a link.
		if tokenType == html.StartTagToken && currentToken.Data == "a" {
			// Loop through all attributes of this anchor tag.
			for _, singleAttribute := range currentToken.Attr {
				// Check if the attribute is the "href" attribute, which holds the link URL.
				if singleAttribute.Key == "href" {
					// Resolve the href value into an absolute URL relative to the current page.
					absoluteResolvedURL := resolveToAbsoluteURL(currentPageURL, singleAttribute.Val)

					// Check if the resolved URL ends with ".pdf" (case-insensitive) using a regular expression.
					pdfExtensionPattern := regexp.MustCompile(`(?i)\.pdf($|\?)`)
					// Check if the URL matches the PDF extension pattern.
					if pdfExtensionPattern.MatchString(absoluteResolvedURL) {
						// Append this URL to the slice of discovered PDF URLs.
						discoveredPdfURLs = append(discoveredPdfURLs, absoluteResolvedURL)
					}
				}
			}
		}
	}

	// Return the slice of discovered PDF URLs to the caller.
	return discoveredPdfURLs
}

// resolveToAbsoluteURL converts a possibly-relative URL into an absolute URL based on the current page's URL.
func resolveToAbsoluteURL(currentPageURL string, hrefValue string) string {
	// Parse the current page's URL into a structured URL object.
	basePageURL, baseParseError := url.Parse(currentPageURL)
	// Check if parsing the base URL failed.
	if baseParseError != nil {
		// Return the raw href value unchanged if we cannot parse the base URL.
		return hrefValue
	}

	// Parse the href value into a structured URL object, which may be relative or absolute.
	hrefAsURL, hrefParseError := url.Parse(hrefValue)
	// Check if parsing the href value failed.
	if hrefParseError != nil {
		// Return the raw href value unchanged if we cannot parse it.
		return hrefValue
	}

	// Resolve the href URL against the base page URL to get a fully absolute URL.
	resolvedURL := basePageURL.ResolveReference(hrefAsURL)

	// Return the string form of the resolved absolute URL.
	return resolvedURL.String()
}

// addCategoryURLToQueueIfNew adds a category URL to the visit queue only if it hasn't been seen before.
func addCategoryURLToQueueIfNew(categoryURL string) {
	// Check if this category URL has already been visited or is already queued.
	if !visitedCategoryURLs[categoryURL] && !isURLAlreadyInSlice(categoryURLsToVisit, categoryURL) {
		// Append the new category URL to the queue since it is genuinely new.
		categoryURLsToVisit = append(categoryURLsToVisit, categoryURL)
	}
}

// addProductURLToQueueIfNew adds a product URL to the visit queue only if it hasn't been seen before.
func addProductURLToQueueIfNew(productURL string) {
	// Check if this product URL has already been visited or is already queued.
	if !visitedProductURLs[productURL] && !isURLAlreadyInSlice(productURLsToVisit, productURL) {
		// Append the new product URL to the queue since it is genuinely new.
		productURLsToVisit = append(productURLsToVisit, productURL)
	}
}

// isURLAlreadyInSlice checks whether a given URL already exists within a slice of URLs.
func isURLAlreadyInSlice(urlSlice []string, targetURL string) bool {
	// Loop through every URL currently in the given slice.
	for _, existingURL := range urlSlice {
		// Check if the current slice entry matches the target URL.
		if existingURL == targetURL {
			// Return true immediately since a match was found.
			return true
		}
	}
	// Return false since no match was found after checking the entire slice.
	return false
}

// downloadPdfFileIfNotAlreadyDownloaded downloads a PDF from a URL, skipping it if already present locally.
func downloadPdfFileIfNotAlreadyDownloaded(pdfURL string) {
	// Parse the PDF URL to extract its path component.
	parsedPdfURL, parseError := url.Parse(pdfURL)
	// Check if parsing the PDF URL failed.
	if parseError != nil {
		// Print the error and stop processing this particular PDF.
		fmt.Println("Error parsing PDF URL:", pdfURL, parseError)
		// Return early since we cannot proceed without a valid parsed URL.
		return
	}

	// Extract just the filename portion (last path segment) from the PDF URL.
	pdfFileName := path.Base(parsedPdfURL.Path)

	// Build the full local filesystem path where this PDF would be saved.
	localPdfFilePath := path.Join(pdfDownloadFolderName, pdfFileName)

	// Check if this filename has already been recorded as downloaded in this run.
	if downloadedPdfFileNames[pdfFileName] {
		// Print a message indicating the duplicate is being skipped, then return.
		fmt.Println("Skipping already-downloaded PDF (in-memory record):", pdfFileName)
		// Return early since this PDF has already been handled.
		return
	}

	// Check if a file with this name already physically exists in the PDFs folder.
	_, statError := os.Stat(localPdfFilePath)
	// Check if the Stat call succeeded, meaning the file already exists on disk.
	if statError == nil {
		// Print a message indicating the duplicate is being skipped, then return.
		fmt.Println("Skipping already-downloaded PDF (found on disk):", pdfFileName)
		// Record this filename as downloaded so future checks are faster.
		downloadedPdfFileNames[pdfFileName] = true
		// Return early since this PDF already exists locally.
		return
	}

	// Print a status message indicating the PDF download is starting.
	fmt.Println("Downloading PDF:", pdfURL)

	// Create an HTTP client with a longer timeout since PDFs may be large files.
	httpClientForPdf := &http.Client{Timeout: 60 * time.Second}
	// Perform the GET request to fetch the PDF file's bytes.
	pdfHttpResponse, pdfRequestError := httpClientForPdf.Get(pdfURL)
	// Check if the HTTP request for the PDF failed.
	if pdfRequestError != nil {
		// Print the error and stop processing this particular PDF.
		fmt.Println("Error downloading PDF:", pdfURL, pdfRequestError)
		// Return early since we cannot proceed without the PDF data.
		return
	}
	// Ensure the PDF response body is closed once this function returns.
	defer pdfHttpResponse.Body.Close()

	// Create a new local file on disk to store the downloaded PDF bytes.
	newLocalPdfFile, fileCreationError := os.Create(localPdfFilePath)
	// Check if creating the local file failed.
	if fileCreationError != nil {
		// Print the error and stop processing this particular PDF.
		fmt.Println("Error creating local PDF file:", localPdfFilePath, fileCreationError)
		// Return early since we cannot save the PDF without a valid file handle.
		return
	}
	// Ensure the local file is closed once this function returns.
	defer newLocalPdfFile.Close()

	// Copy the bytes from the HTTP response body into the local file.
	_, copyError := io.Copy(newLocalPdfFile, pdfHttpResponse.Body)
	// Check if copying the PDF bytes to disk failed.
	if copyError != nil {
		// Print the error since the save operation did not complete successfully.
		fmt.Println("Error saving PDF to disk:", localPdfFilePath, copyError)
		// Return early since the file may be incomplete or corrupted.
		return
	}

	// Record this filename as downloaded so it will not be downloaded again in this run.
	downloadedPdfFileNames[pdfFileName] = true

	// Print a confirmation message indicating the PDF was saved successfully.
	fmt.Println("Saved PDF to:", localPdfFilePath)
}
