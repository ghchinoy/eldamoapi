# Eldamo API

An API and related tools around Paul Strack's [Eldamo](http://eldamo.org/) lexicon compilation for J. R. R. Tolkien's languages.


## Eldamo

More information can be found on [eldamo.org](http://eldamo.org/).


## Generating Go structs from XSD

To create Go structs from an XSD, use [go-xsd](https://github.com/metaleap/go-xsd)

	go get github.com/metaleap/go-xsd
	go get github.com/metaleap/go-buildrun
	xsd-makepkg -uri="127.0.0.1:4909/eldamo.xsd"

Creates in `$GOHOME/src/github.com/metaleap/go-xsd-pkg/127.0.0.1:4909/`


## Scripts

Utility scripts for analysis

* `getTools` - downloads required tools for creating dtd, xsd, and html version of schema
* `generateHtmlFromXml` - generates HTML from XSD using XSL


`getTools` - downloads saxon, trang, and xs3p to the `tools` directory

`generateHtmlFromXml` expects a `tools` directory with java/xsl tools and an `eldamo` directory with the `eldamo-data.xml` file. Providing the `s` flag (`generateHtmlFromXml -s`) will download the eldamo-data.xml file

### Tools

The `tools` directory (built by `getTools` script) contains external tools to process the Eldamo XML source.

* [Saxon](http://sourceforge.net/projects/saxon/) - XSLT &amp; XQuery processor and [DTD Generator](http://sourceforge.net/projects/saxon/files/DTDGenerator/)
* [xs3p](http://sourceforge.net/projects/xs3p/) - Schema document HTML XSLT 
* [trang](http://www.thaiopensource.com/relaxng/trang-manual.html) - more generic schema converter
* [Linguistic Library](http://linguisticlibrary.org/) and [Sindarin Library](http://sindarinlibrary.com/) ([forums](http://sindarinlibrary.boards.net/))
