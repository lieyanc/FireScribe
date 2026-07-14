package exporter

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

func renderDOCX(title string, pages []preparedPage, includePageNumbers bool) ([]byte, error) {
	var document strings.Builder
	document.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	document.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	if strings.TrimSpace(title) != "" {
		document.WriteString(docxParagraph(title, "Title"))
	}
	for index, page := range pages {
		if index > 0 {
			document.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
		}
		if includePageNumbers {
			document.WriteString(docxParagraph(fmt.Sprintf("第 %d 页", page.PageNo), "Heading1"))
		}
		for _, paragraph := range splitDocumentParagraphs(page.Text) {
			document.WriteString(docxParagraph(paragraph, "Normal"))
		}
		if len(page.Notes) > 0 {
			document.WriteString(docxParagraph("审校记录", "Heading2"))
			for _, note := range page.Notes {
				document.WriteString(docxParagraph("• "+note, "ExportNote"))
			}
		}
	}
	document.WriteString(`<w:sectPr><w:footerReference w:type="default" r:id="rId4" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"/><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>`)
	document.WriteString(`</w:body></w:document>`)

	parts := map[string]string{
		"[Content_Types].xml":          docxContentTypes,
		"_rels/.rels":                  docxPackageRelationships,
		"docProps/core.xml":            docxCoreProperties(title),
		"docProps/app.xml":             docxAppProperties,
		"word/document.xml":            document.String(),
		"word/styles.xml":              docxStyles,
		"word/settings.xml":            docxSettings,
		"word/fontTable.xml":           docxFontTable,
		"word/footer1.xml":             docxFooter,
		"word/_rels/document.xml.rels": docxDocumentRelationships,
	}
	var output bytes.Buffer
	zw := zip.NewWriter(&output)
	for _, name := range []string{
		"[Content_Types].xml", "_rels/.rels", "docProps/core.xml", "docProps/app.xml",
		"word/document.xml", "word/styles.xml", "word/settings.xml", "word/fontTable.xml",
		"word/footer1.xml", "word/_rels/document.xml.rels",
	} {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Unix(0, 0).UTC())
		writer, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := io.WriteString(writer, parts[name]); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func splitDocumentParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

func docxParagraph(text, style string) string {
	var out strings.Builder
	out.WriteString(`<w:p><w:pPr><w:pStyle w:val="`)
	out.WriteString(style)
	out.WriteString(`"/></w:pPr>`)
	if text != "" {
		out.WriteString(`<w:r><w:t xml:space="preserve">`)
		out.WriteString(xmlEscape(text))
		out.WriteString(`</w:t></w:r>`)
	}
	out.WriteString(`</w:p>`)
	return out.String()
}

func xmlEscape(value string) string {
	var clean strings.Builder
	for _, r := range value {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 {
			clean.WriteRune(r)
		}
	}
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(clean.String()))
	return escaped.String()
}

func docxCoreProperties(title string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + xmlEscape(title) + `</dc:title><dc:creator>FireScribe</dc:creator><cp:lastModifiedBy>FireScribe</cp:lastModifiedBy>` +
		`</cp:coreProperties>`
}

const docxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
  <Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>
  <Override PartName="/word/fontTable.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.fontTable+xml"/>
  <Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
  <Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>
</Types>`

const docxPackageRelationships = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>
</Relationships>`

const docxDocumentRelationships = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable" Target="fontTable.xml"/>
  <Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>
</Relationships>`

// standard_business_brief preset, resolved explicitly for a transcription:
// Letter/1in margins, 11pt body, restrained blue hierarchy, 6pt paragraph
// spacing, and a compact gold review-note callout.
const docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Aptos" w:hAnsi="Aptos" w:eastAsia="Microsoft YaHei" w:cs="Aptos"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="zh-CN" w:eastAsia="zh-CN"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:before="0" w:after="120" w:line="276" w:lineRule="auto"/></w:pPr><w:rPr><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:style>
  <w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:before="0" w:after="240"/></w:pPr><w:rPr><w:b/><w:color w:val="1F4D78"/><w:sz w:val="40"/><w:szCs w:val="40"/></w:rPr></w:style>
  <w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:outlineLvl w:val="0"/><w:pPr><w:keepNext/><w:spacing w:before="320" w:after="160"/></w:pPr><w:rPr><w:b/><w:color w:val="2E74B5"/><w:sz w:val="32"/><w:szCs w:val="32"/></w:rPr></w:style>
  <w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:outlineLvl w:val="1"/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/></w:pPr><w:rPr><w:b/><w:color w:val="2E74B5"/><w:sz w:val="26"/><w:szCs w:val="26"/></w:rPr></w:style>
  <w:style w:type="paragraph" w:styleId="ExportNote"><w:name w:val="Export Note"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="40" w:after="80" w:line="280" w:lineRule="auto"/><w:ind w:left="240" w:right="120"/><w:shd w:val="clear" w:color="auto" w:fill="FFF7D6"/><w:pBdr><w:left w:val="single" w:sz="16" w:space="8" w:color="D6A800"/></w:pBdr></w:pPr><w:rPr><w:color w:val="7A5A00"/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr></w:style>
</w:styles>`

const docxSettings = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:zoom w:percent="100"/><w:defaultTabStop w:val="720"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`
const docxFontTable = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:fonts xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:font w:name="Aptos"/><w:font w:name="Microsoft YaHei"><w:charset w:val="86"/><w:family w:val="swiss"/></w:font></w:fonts>`
const docxFooter = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="right"/></w:pPr><w:r><w:rPr><w:color w:val="7F8C8D"/><w:sz w:val="18"/></w:rPr><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:ftr>`
const docxAppProperties = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>FireScribe</Application><DocSecurity>0</DocSecurity><ScaleCrop>false</ScaleCrop></Properties>`
