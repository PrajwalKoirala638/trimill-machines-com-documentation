// main.go implements a sequential (non-concurrent) web scraper for Trimill Machines product PDFs,
// using only official Go standard library packages.
package main

// Import the required standard library packages for this program.
import (
	"io"       // io provides basic input/output utilities like copying streams.
	"log"      // log provides timestamped logging output, replacing fmt for status messages.
	"net/http" // net/http lets us make HTTP requests to fetch web pages and files.
	"net/url"  // net/url helps parse and resolve URLs relative to a base URL.
	"os"       // os lets us interact with the filesystem, like creating folders and files.
	"path"     // path helps manipulate URL/file paths safely.
	"regexp"   // regexp lets us use regular expressions to find href attributes in raw HTML.
	"strings"  // strings provides helper functions for string manipulation.
	"time"     // time lets us add delays between requests to be polite to the server.
)

// baseWebsiteURL is the root URL of the Trimill Machines website.
const baseWebsiteURL = "https://www.trimill-machines.com"

// productsPageURL is the starting page that lists top-level product categories.
const productsPageURL = "https://www.trimill-machines.com/products/"

// pdfDownloadFolderName is the local folder where downloaded PDFs will be saved.
const pdfDownloadFolderName = "PDFs"

// politeDelayBetweenRequests is how long we pause between HTTP requests to avoid hammering the server.
const politeDelayBetweenRequests = 500 * time.Millisecond

// hrefAttributePattern is a compiled regular expression that matches href="..." or href='...' attributes.
// It captures the URL inside either double or single quotes, in an HTML-standard-library-free way.
var hrefAttributePattern = regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+)["']`)

// pdfFileExtensionPattern is a compiled regular expression that matches URLs ending in ".pdf" (optionally
// followed by a query string), case-insensitively.
var pdfFileExtensionPattern = regexp.MustCompile(`(?i)\.pdf($|\?)`)

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
	// Configure the log package to include the date and time on every log line.
	log.SetFlags(log.Ldate | log.Ltime)

	// Create the local PDFs folder if it does not already exist, with standard permissions.
	createPdfFolderIfNotExists()

	// Record the number of already-downloaded PDF files found on disk before scraping starts.
	loadExistingDownloadedPdfFileNamesFromDisk()

	// Fetch the raw HTML content of the main products page.
	initialPageHtmlContent, initialPageFetchError := fetchHtmlContentFromURL(productsPageURL)
	// Check if there was an error fetching the initial products page.
	if initialPageFetchError != nil {
		// Log the fatal error and stop the program since we cannot proceed without the first page.
		log.Fatalf("Error fetching products page %s: %v", productsPageURL, initialPageFetchError)
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

	// Process every category discovered, sequentially, adding newly found categories/products as we go.
	processAllQueuedCategoryURLs()

	// Process every product discovered, sequentially, downloading any PDFs found on each page.
	processAllQueuedProductURLs()

	// Log a final message once all categories, products, and PDFs have been processed.
	log.Printf("Scraping complete. All discovered PDFs have been downloaded to the %s folder.", pdfDownloadFolderName)
}

// processAllQueuedCategoryURLs sequentially visits every category URL in the queue, discovering and
// enqueueing any further category or product URLs found, until the queue is empty.
func processAllQueuedCategoryURLs() {
	// Continue processing categories as long as there are unvisited categories in the queue.
	for len(categoryURLsToVisit) > 0 {
		// Remove and return the first category URL from the front of the queue.
		currentCategoryURL := categoryURLsToVisit[0]
		// Reslice the queue to remove the URL we just took out.
		categoryURLsToVisit = categoryURLsToVisit[1:]

		// Check whether this category URL has somehow already been visited (defensive safety check).
		if visitedCategoryURLs[currentCategoryURL] {
			// Skip this URL since it has already been processed.
			continue
		}

		// Mark this category URL as visited so we never process it again.
		visitedCategoryURLs[currentCategoryURL] = true

		// Log a status message showing which category is being visited.
		log.Printf("Visiting category: %s", currentCategoryURL)

		// Pause briefly between requests to avoid overwhelming the server.
		time.Sleep(politeDelayBetweenRequests)

		// Fetch the HTML content of the current category page.
		categoryPageHtmlContent, categoryFetchError := fetchHtmlContentFromURL(currentCategoryURL)
		// Check if there was an error fetching this particular category page.
		if categoryFetchError != nil {
			// Log the error but continue with other categories rather than stopping entirely.
			log.Printf("Error fetching category page %s: %v", currentCategoryURL, categoryFetchError)
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
}

// processAllQueuedProductURLs sequentially visits every product URL in the queue and downloads any
// PDF files linked from each product page, until the queue is empty.
func processAllQueuedProductURLs() {
	// Continue processing products as long as there are unvisited products in the queue.
	for len(productURLsToVisit) > 0 {
		// Remove and return the first product URL from the front of the queue.
		currentProductURL := productURLsToVisit[0]
		// Reslice the queue to remove the URL we just took out.
		productURLsToVisit = productURLsToVisit[1:]

		// Check whether this product URL has somehow already been visited (defensive safety check).
		if visitedProductURLs[currentProductURL] {
			// Skip this URL since it has already been processed.
			continue
		}

		// Mark this product URL as visited so we never process it again.
		visitedProductURLs[currentProductURL] = true

		// Log a status message showing which product page is being visited.
		log.Printf("Visiting product page: %s", currentProductURL)

		// Pause briefly between requests to avoid overwhelming the server.
		time.Sleep(politeDelayBetweenRequests)

		// Fetch the HTML content of the current product page.
		productPageHtmlContent, productFetchError := fetchHtmlContentFromURL(currentProductURL)
		// Check if there was an error fetching this particular product page.
		if productFetchError != nil {
			// Log the error but continue with other products rather than stopping entirely.
			log.Printf("Error fetching product page %s: %v", currentProductURL, productFetchError)
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
}

// createPdfFolderIfNotExists creates the local PDFs directory if it doesn't already exist.
func createPdfFolderIfNotExists() {
	// Attempt to create the directory (and any needed parents) with read/write/execute permissions.
	folderCreationError := os.MkdirAll(pdfDownloadFolderName, 0755)
	// Check if an error occurred while creating the folder.
	if folderCreationError != nil {
		// Log the fatal error since we cannot save PDFs without this folder.
		log.Fatalf("Error creating PDF folder %s: %v", pdfDownloadFolderName, folderCreationError)
	}
}

// loadExistingDownloadedPdfFileNamesFromDisk scans the PDFs folder and records any files already
// present, so that PDFs downloaded in previous runs are recognized and skipped in this run too.
func loadExistingDownloadedPdfFileNamesFromDisk() {
	// Read the list of directory entries currently inside the PDFs folder.
	existingDirectoryEntries, directoryReadError := os.ReadDir(pdfDownloadFolderName)
	// Check if reading the directory failed.
	if directoryReadError != nil {
		// Log the error but continue, since a fresh empty folder is not a fatal problem.
		log.Printf("Warning: could not read existing PDF folder %s: %v", pdfDownloadFolderName, directoryReadError)
		// Return early since there is nothing further to load.
		return
	}

	// Loop through every entry found in the PDFs folder.
	for _, singleDirectoryEntry := range existingDirectoryEntries {
		// Check that this entry is a regular file and not a subdirectory.
		if !singleDirectoryEntry.IsDir() {
			// Record this existing filename as already downloaded.
			downloadedPdfFileNames[singleDirectoryEntry.Name()] = true
		}
	}

	// Log how many pre-existing PDF files were found, for visibility.
	log.Printf("Found %d existing PDF file(s) already on disk.", len(downloadedPdfFileNames))
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

	// Check whether the server responded with a non-successful HTTP status code.
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		// Read the response body into a byte slice so we can report on the failed request.
		responseBodyBytes, _ := io.ReadAll(httpResponse.Body)
		// Return the partial body text and a descriptive error about the bad status code.
		return string(responseBodyBytes), &httpStatusCodeError{URL: targetURL, StatusCode: httpResponse.StatusCode}
	}

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

// httpStatusCodeError is a custom error type describing a non-2xx HTTP response for a given URL.
type httpStatusCodeError struct {
	URL        string // URL is the address that returned the unsuccessful status code.
	StatusCode int    // StatusCode is the HTTP status code that was returned.
}

// Error implements the standard error interface for httpStatusCodeError, producing a readable message.
func (thisError *httpStatusCodeError) Error() string {
	// Build and return a human-readable description of the failed HTTP request.
	return "unexpected HTTP status " + http.StatusText(thisError.StatusCode) + " for URL " + thisError.URL
}

// extractAllHrefValuesFromHtml scans raw HTML text and returns every href attribute value found,
// using only the standard library "regexp" package rather than an external HTML parser.
func extractAllHrefValuesFromHtml(htmlContent string) []string {
	// Create an empty slice to hold every raw href value discovered in the HTML.
	allHrefValuesFound := []string{}

	// Find every match of the href attribute pattern in the HTML content, with capture groups included.
	allRegularExpressionMatches := hrefAttributePattern.FindAllStringSubmatch(htmlContent, -1)

	// Loop through each match found by the regular expression.
	for _, singleMatch := range allRegularExpressionMatches {
		// Check that the match contains at least the full match plus one capture group.
		if len(singleMatch) >= 2 {
			// Append the captured href value (the URL inside the quotes) to our results slice.
			allHrefValuesFound = append(allHrefValuesFound, singleMatch[1])
		}
	}

	// Return the complete slice of raw href values found in the HTML.
	return allHrefValuesFound
}

// extractCategoryAndProductLinksFromHtml scans HTML and returns slices of category and product URLs found.
func extractCategoryAndProductLinksFromHtml(htmlContent string, currentPageURL string) ([]string, []string) {
	// Create an empty slice to hold category URLs discovered on this page.
	discoveredCategoryURLs := []string{}
	// Create an empty slice to hold product URLs discovered on this page.
	discoveredProductURLs := []string{}

	// Extract every raw href value present in the HTML content.
	allRawHrefValues := extractAllHrefValuesFromHtml(htmlContent)

	// Loop through each raw href value found on the page.
	for _, singleRawHrefValue := range allRawHrefValues {
		// Resolve the href value into an absolute URL relative to the current page.
		absoluteResolvedURL := resolveToAbsoluteURL(currentPageURL, singleRawHrefValue)

		// Check if the resolved URL belongs to the Trimill Machines website at all.
		if !strings.HasPrefix(absoluteResolvedURL, baseWebsiteURL) {
			// Skip this URL since it points to an external site.
			continue
		}

		// Check if the resolved URL is a category page URL.
		if strings.Contains(absoluteResolvedURL, "/category/") {
			// Append this URL to the slice of discovered category URLs.
			discoveredCategoryURLs = append(discoveredCategoryURLs, absoluteResolvedURL)
		} else if strings.Contains(absoluteResolvedURL, "/products/") && !strings.EqualFold(absoluteResolvedURL, productsPageURL) {
			// Append this URL to the slice of discovered product URLs, excluding the main products page itself.
			discoveredProductURLs = append(discoveredProductURLs, absoluteResolvedURL)
		}
	}

	// Return both slices of discovered URLs to the caller.
	return discoveredCategoryURLs, discoveredProductURLs
}

// extractPdfLinksFromHtml scans HTML and returns a slice of absolute PDF URLs found on the page.
func extractPdfLinksFromHtml(htmlContent string, currentPageURL string) []string {
	// Create an empty slice to hold PDF URLs discovered on this page.
	discoveredPdfURLs := []string{}

	// Extract every raw href value present in the HTML content.
	allRawHrefValues := extractAllHrefValuesFromHtml(htmlContent)

	// Loop through each raw href value found on the page.
	for _, singleRawHrefValue := range allRawHrefValues {
		// Resolve the href value into an absolute URL relative to the current page.
		absoluteResolvedURL := resolveToAbsoluteURL(currentPageURL, singleRawHrefValue)

		// Check if the resolved URL matches the PDF file extension pattern.
		if pdfFileExtensionPattern.MatchString(absoluteResolvedURL) {
			// Append this URL to the slice of discovered PDF URLs.
			discoveredPdfURLs = append(discoveredPdfURLs, absoluteResolvedURL)
		}
	}

	// Return the slice of discovered PDF URLs to the caller.
	return discoveredPdfURLs
}

// resolveToAbsoluteURL converts a possibly-relative URL into an absolute URL based on the current page's URL.
func resolveToAbsoluteURL(currentPageURL string, hrefValue string) string {
	// Trim any surrounding whitespace from the href value before parsing it.
	trimmedHrefValue := strings.TrimSpace(hrefValue)

	// Check if the href value is empty or is just a page anchor/fragment, which we should ignore.
	if trimmedHrefValue == "" || strings.HasPrefix(trimmedHrefValue, "#") {
		// Return an empty string to signal there is nothing useful to resolve.
		return ""
	}

	// Parse the current page's URL into a structured URL object.
	basePageURL, baseParseError := url.Parse(currentPageURL)
	// Check if parsing the base URL failed.
	if baseParseError != nil {
		// Return the raw href value unchanged if we cannot parse the base URL.
		return trimmedHrefValue
	}

	// Parse the href value into a structured URL object, which may be relative or absolute.
	hrefAsURL, hrefParseError := url.Parse(trimmedHrefValue)
	// Check if parsing the href value failed.
	if hrefParseError != nil {
		// Return the raw href value unchanged if we cannot parse it.
		return trimmedHrefValue
	}

	// Resolve the href URL against the base page URL to get a fully absolute URL.
	resolvedURL := basePageURL.ResolveReference(hrefAsURL)

	// Return the string form of the resolved absolute URL.
	return resolvedURL.String()
}

// addCategoryURLToQueueIfNew adds a category URL to the visit queue only if it hasn't been seen before.
func addCategoryURLToQueueIfNew(categoryURL string) {
	// Check if the category URL is empty, meaning it should be ignored entirely.
	if categoryURL == "" {
		// Return early since there is nothing valid to add.
		return
	}

	// Check if this category URL has already been visited or is already queued.
	if !visitedCategoryURLs[categoryURL] && !isURLAlreadyInSlice(categoryURLsToVisit, categoryURL) {
		// Append the new category URL to the queue since it is genuinely new.
		categoryURLsToVisit = append(categoryURLsToVisit, categoryURL)
		// Log that a brand-new category URL was queued for visiting.
		log.Printf("Queued new category URL: %s", categoryURL)
	}
}

// addProductURLToQueueIfNew adds a product URL to the visit queue only if it hasn't been seen before.
func addProductURLToQueueIfNew(productURL string) {
	// Check if the product URL is empty, meaning it should be ignored entirely.
	if productURL == "" {
		// Return early since there is nothing valid to add.
		return
	}

	// Check if this product URL has already been visited or is already queued.
	if !visitedProductURLs[productURL] && !isURLAlreadyInSlice(productURLsToVisit, productURL) {
		// Append the new product URL to the queue since it is genuinely new.
		productURLsToVisit = append(productURLsToVisit, productURL)
		// Log that a brand-new product URL was queued for visiting.
		log.Printf("Queued new product URL: %s", productURL)
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
		// Log the error and stop processing this particular PDF.
		log.Printf("Error parsing PDF URL %s: %v", pdfURL, parseError)
		// Return early since we cannot proceed without a valid parsed URL.
		return
	}

	// Extract just the filename portion (last path segment) from the PDF URL.
	pdfFileName := path.Base(parsedPdfURL.Path)

	// Build the full local filesystem path where this PDF would be saved.
	localPdfFilePath := path.Join(pdfDownloadFolderName, pdfFileName)

	// Check if this filename has already been recorded as downloaded in this run or a previous one.
	if downloadedPdfFileNames[pdfFileName] {
		// Log a message indicating the duplicate is being skipped, then return.
		log.Printf("Skipping already-downloaded PDF: %s", pdfFileName)
		// Return early since this PDF has already been handled.
		return
	}

	// Log a status message indicating the PDF download is starting.
	log.Printf("Downloading PDF: %s", pdfURL)

	// Pause briefly before this request to avoid overwhelming the server.
	time.Sleep(politeDelayBetweenRequests)

	// Create an HTTP client with a longer timeout since PDFs may be large files.
	httpClientForPdf := &http.Client{Timeout: 60 * time.Second}
	// Perform the GET request to fetch the PDF file's bytes.
	pdfHttpResponse, pdfRequestError := httpClientForPdf.Get(pdfURL)
	// Check if the HTTP request for the PDF failed.
	if pdfRequestError != nil {
		// Log the error and stop processing this particular PDF.
		log.Printf("Error downloading PDF %s: %v", pdfURL, pdfRequestError)
		// Return early since we cannot proceed without the PDF data.
		return
	}
	// Ensure the PDF response body is closed once this function returns.
	defer pdfHttpResponse.Body.Close()

	// Check whether the server responded with a non-successful HTTP status code for the PDF request.
	if pdfHttpResponse.StatusCode < 200 || pdfHttpResponse.StatusCode >= 300 {
		// Log the error describing the failed PDF download and stop processing this file.
		log.Printf("Error downloading PDF %s: unexpected HTTP status %s", pdfURL, http.StatusText(pdfHttpResponse.StatusCode))
		// Return early since there is no valid PDF content to save.
		return
	}

	// Create a temporary file path used while the PDF is still being written, to avoid partial files
	// being mistaken for completed downloads if the program is interrupted mid-write.
	temporaryPdfFilePath := localPdfFilePath + ".partial"

	// Create a new local file on disk to store the downloaded PDF bytes temporarily.
	newLocalPdfFile, fileCreationError := os.Create(temporaryPdfFilePath)
	// Check if creating the local file failed.
	if fileCreationError != nil {
		// Log the error and stop processing this particular PDF.
		log.Printf("Error creating local PDF file %s: %v", temporaryPdfFilePath, fileCreationError)
		// Return early since we cannot save the PDF without a valid file handle.
		return
	}

	// Copy the bytes from the HTTP response body into the local temporary file.
	_, copyError := io.Copy(newLocalPdfFile, pdfHttpResponse.Body)
	// Close the local file immediately after copying, regardless of whether copying succeeded.
	newLocalPdfFile.Close()
	// Check if copying the PDF bytes to disk failed.
	if copyError != nil {
		// Log the error since the save operation did not complete successfully.
		log.Printf("Error saving PDF to disk %s: %v", temporaryPdfFilePath, copyError)
		// Remove the incomplete temporary file so it does not linger on disk.
		os.Remove(temporaryPdfFilePath)
		// Return early since the file may be incomplete or corrupted.
		return
	}

	// Rename the completed temporary file to its final intended filename.
	renameError := os.Rename(temporaryPdfFilePath, localPdfFilePath)
	// Check if renaming the temporary file failed.
	if renameError != nil {
		// Log the error since the file could not be finalized.
		log.Printf("Error finalizing PDF file %s: %v", localPdfFilePath, renameError)
		// Return early since the download did not complete successfully.
		return
	}

	// Record this filename as downloaded so it will not be downloaded again in this run.
	downloadedPdfFileNames[pdfFileName] = true

	// Log a confirmation message indicating the PDF was saved successfully.
	log.Printf("Saved PDF to: %s", localPdfFilePath)
}
