package geography

import (
	"fmt"
	"sort"
	"strings"
)

type Country struct {
	ISO2 string `json:"iso2"`
	ISO3 string `json:"iso3"`
	Name string `json:"name"`
}

type Area struct {
	CountryISO2 string `json:"countryISO2"`
	ParentCode  string `json:"parentCode,omitempty"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Level       int    `json:"level"`
	AreaType    string `json:"areaType"`
}

type Catalogue struct {
	countries []Country
	areas     []Area
}

func DefaultCatalogue() *Catalogue {
	countries := []Country{
		{ISO2: "NG", ISO3: "NGA", Name: "Nigeria"},
		{ISO2: "GH", ISO3: "GHA", Name: "Ghana"},
		{ISO2: "GB", ISO3: "GBR", Name: "United Kingdom"},
	}
	nigerianStates := []string{
		"Abia", "Adamawa", "Akwa Ibom", "Anambra", "Bauchi", "Bayelsa", "Benue", "Borno",
		"Cross River", "Delta", "Ebonyi", "Edo", "Ekiti", "Enugu", "Federal Capital Territory",
		"Gombe", "Imo", "Jigawa", "Kaduna", "Kano", "Katsina", "Kebbi", "Kogi", "Kwara",
		"Lagos", "Nasarawa", "Niger", "Ogun", "Ondo", "Osun", "Oyo", "Plateau", "Rivers",
		"Sokoto", "Taraba", "Yobe", "Zamfara",
	}
	areas := make([]Area, 0, len(nigerianStates)+20)
	for _, state := range nigerianStates {
		areas = append(areas, Area{CountryISO2: "NG", Code: canonicalCode(state), Name: state, Level: 1, AreaType: "STATE"})
	}
	for _, lga := range []string{
		"Agege", "Ajeromi-Ifelodun", "Alimosho", "Amuwo-Odofin", "Apapa", "Badagry", "Epe",
		"Eti-Osa", "Ibeju-Lekki", "Ifako-Ijaiye", "Ikeja", "Ikorodu", "Kosofe", "Lagos Island",
		"Lagos Mainland", "Mushin", "Ojo", "Oshodi-Isolo", "Shomolu", "Surulere",
	} {
		areas = append(areas, Area{CountryISO2: "NG", ParentCode: "LAGOS", Code: canonicalCode(lga), Name: lga, Level: 2, AreaType: "LGA"})
	}
	return &Catalogue{countries: countries, areas: areas}
}

func (c *Catalogue) Countries() []Country {
	out := append([]Country(nil), c.countries...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *Catalogue) Areas(countryISO2, parentCode string, level int) []Area {
	var out []Area
	for _, area := range c.areas {
		if countryISO2 != "" && !strings.EqualFold(area.CountryISO2, countryISO2) {
			continue
		}
		if parentCode != "" && !strings.EqualFold(area.ParentCode, parentCode) {
			continue
		}
		if level > 0 && area.Level != level {
			continue
		}
		out = append(out, area)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *Catalogue) Validate(countryISO2, state, lga string) error {
	countryISO2 = strings.ToUpper(strings.TrimSpace(countryISO2))
	if state == "" && lga == "" {
		return nil
	}
	var stateArea *Area
	for i := range c.areas {
		area := &c.areas[i]
		if area.Level == 1 && strings.EqualFold(area.CountryISO2, countryISO2) &&
			(strings.EqualFold(area.Name, state) || strings.EqualFold(area.Code, state)) {
			stateArea = area
			break
		}
	}
	if stateArea == nil {
		return fmt.Errorf("state or region %q is not configured for country %s", state, countryISO2)
	}
	if lga == "" {
		return nil
	}
	for _, area := range c.areas {
		if area.Level == 2 && strings.EqualFold(area.CountryISO2, countryISO2) &&
			strings.EqualFold(area.ParentCode, stateArea.Code) &&
			(strings.EqualFold(area.Name, lga) || strings.EqualFold(area.Code, lga)) {
			return nil
		}
	}
	return fmt.Errorf("LGA or district %q is not configured under %s", lga, stateArea.Name)
}

func canonicalCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "_", "/", "_", "-", "_", "'", "")
	return replacer.Replace(value)
}
