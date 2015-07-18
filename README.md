# Eldamo API

An API and related tools around Paul Strack's [Eldamo](http://eldamo.org/) lexicon compilation for J. R. R. Tolkien's languages.


## Eldamo

More information can be found on [eldamo.org](http://eldamo.org/).

The `eldamo` directory contains a snapshot of the source provided by Paul Strack.

## Tools

The `tools` directory contains external tools to process the Eldamo source.

* [Saxon](http://sourceforge.net/projects/saxon/) - XSLT &amp; XQuery processor and [DTD Generator](http://sourceforge.net/projects/saxon/files/DTDGenerator/)
* [xs3p](http://sourceforge.net/projects/xs3p/) - Schema document generator
* [trang](http://www.thaiopensource.com/relaxng/trang-manual.html) - schema converter
* [Linguistic Library](http://linguisticlibrary.org/) and [Sindarin Library](http://sindarinlibrary.com/) ([forums](http://sindarinlibrary.boards.net/))

XML to DTD

	java -cp tools/dtdgen/dtdgen.jar DTDGenerator eldamo/eldamo-data.xml > eldamo.dtd

DTD to XSD

	java -jar tools/trang-20081028/trang.jar -I dtd -O xsd eldamo.dtd eldamo2.xsd


XSD to HTML

	java -jar tools/saxon/saxon9he.jar -o:eldamo.html eldamo.xsd tools/xs3p/xs3p.xsl title="Eldamo Schema Documentation"