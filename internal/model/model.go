package model

import "encoding/xml"

type Json_result struct {
	Manufacturer    string   `json:"manufacturer"`
	Logical_path    string   `json:"logical_path"`
	Date_extraction string   `json:"date_extraction"`
	Product_Type    string   `json:"product_type"`
	Version         string   `json:"version"`
	Hash            string   `json:"hash"`
	//Directory       []string `json:"directory"`
	Extraction_type string   `json:"extraction_type"`
	Packages        []string `json:"packages"`
}

type Packages struct {
	XMLName xml.Name   `xml:"packages"`
	Package []PkgEntry `xml:"package"`
}

type PkgEntry struct {
	Name    string `xml:"name,attr"`
	Version string `xml:"version,attr"`
}

type PackageXML struct {
	Name string `xml:"name,attr"`
}

type PackagesXML struct {
	Items []PackageXML `xml:"package"`
}