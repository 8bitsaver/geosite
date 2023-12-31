package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/sagernet/sing-box/common/geosite"
	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

var (
	datFile          string
	dbOutputDir      string
	ruleSetOutputDir string
)

func init() {
	currentDir, _ := os.Getwd()
	flag.StringVar(&datFile, "dat-in", "dlc.dat", "Path to the dlc.dat(github.com/v2fly/domain-list-community)")
	flag.StringVar(&dbOutputDir, "sdb-out", currentDir, "Output path to the sing-box db.")
	flag.StringVar(&ruleSetOutputDir, "srs-out", currentDir, "Output path to the sing-box ruleset.")
	flag.Parse()
}

func parse(vGeositeData []byte) (map[string][]geosite.Item, error) {
	vGeositeList := routercommon.GeoSiteList{}
	err := proto.Unmarshal(vGeositeData, &vGeositeList)
	if err != nil {
		return nil, err
	}
	domainMap := make(map[string][]geosite.Item)
	for _, vGeositeEntry := range vGeositeList.Entry {
		code := strings.ToLower(vGeositeEntry.CountryCode)
		domains := make([]geosite.Item, 0, len(vGeositeEntry.Domain)*2)
		attributes := make(map[string][]*routercommon.Domain)
		for _, domain := range vGeositeEntry.Domain {
			if len(domain.Attribute) > 0 {
				for _, attribute := range domain.Attribute {
					attributes[attribute.Key] = append(attributes[attribute.Key], domain)
				}
			}
			switch domain.Type {
			case routercommon.Domain_Plain:
				domains = append(domains, geosite.Item{
					Type:  geosite.RuleTypeDomainKeyword,
					Value: domain.Value,
				})
			case routercommon.Domain_Regex:
				domains = append(domains, geosite.Item{
					Type:  geosite.RuleTypeDomainRegex,
					Value: domain.Value,
				})
			case routercommon.Domain_RootDomain:
				if strings.Contains(domain.Value, ".") {
					domains = append(domains, geosite.Item{
						Type:  geosite.RuleTypeDomain,
						Value: domain.Value,
					})
				}
				domains = append(domains, geosite.Item{
					Type:  geosite.RuleTypeDomainSuffix,
					Value: "." + domain.Value,
				})
			case routercommon.Domain_Full:
				domains = append(domains, geosite.Item{
					Type:  geosite.RuleTypeDomain,
					Value: domain.Value,
				})
			}
		}
		domainMap[code] = common.Uniq(domains)
		for attribute, attributeEntries := range attributes {
			attributeDomains := make([]geosite.Item, 0, len(attributeEntries)*2)
			for _, domain := range attributeEntries {
				switch domain.Type {
				case routercommon.Domain_Plain:
					attributeDomains = append(attributeDomains, geosite.Item{
						Type:  geosite.RuleTypeDomainKeyword,
						Value: domain.Value,
					})
				case routercommon.Domain_Regex:
					attributeDomains = append(attributeDomains, geosite.Item{
						Type:  geosite.RuleTypeDomainRegex,
						Value: domain.Value,
					})
				case routercommon.Domain_RootDomain:
					if strings.Contains(domain.Value, ".") {
						attributeDomains = append(attributeDomains, geosite.Item{
							Type:  geosite.RuleTypeDomain,
							Value: domain.Value,
						})
					}
					attributeDomains = append(attributeDomains, geosite.Item{
						Type:  geosite.RuleTypeDomainSuffix,
						Value: "." + domain.Value,
					})
				case routercommon.Domain_Full:
					attributeDomains = append(attributeDomains, geosite.Item{
						Type:  geosite.RuleTypeDomain,
						Value: domain.Value,
					})
				}
			}
			domainMap[code+"@"+attribute] = common.Uniq(attributeDomains)
		}
	}
	return domainMap, nil
}

func generate(output string, cnOutput string, ruleSetOutput string) error {
	vData, err := os.ReadFile(filepath.Join(datFile))
	if err != nil {
		log.Error("fail to open %s\n", err)
		return err
	}
	domainMap, err := parse(vData)
	if err != nil {
		return err
	}
	outputPath, _ := filepath.Abs(output)
	os.Stderr.WriteString("write " + outputPath + "\n")
	outputFile, err := os.Create(filepath.Join(dbOutputDir, output))
	if err != nil {
		return err
	}
	defer outputFile.Close()
	err = geosite.Write(outputFile, domainMap)
	if err != nil {
		return err
	}
	cnCodes := []string{
		"cn",
		"geolocation-!cn",
		"category-companies@cn",
	}
	cnDomainMap := make(map[string][]geosite.Item)
	for _, cnCode := range cnCodes {
		cnDomainMap[cnCode] = domainMap[cnCode]
	}
	cnOutputFile, err := os.Create(filepath.Join(dbOutputDir, cnOutput))
	if err != nil {
		return err
	}
	defer cnOutputFile.Close()
	err = geosite.Write(cnOutputFile, cnDomainMap)
	if err != nil {
		return err
	}
	os.RemoveAll(filepath.Join(ruleSetOutputDir, ruleSetOutput))
	err = os.MkdirAll(filepath.Join(ruleSetOutputDir, ruleSetOutput), 0o755)
	if err != nil {
		return err
	}
	for code, domains := range domainMap {
		var headlessRule option.DefaultHeadlessRule
		defaultRule := geosite.Compile(domains)
		headlessRule.Domain = defaultRule.Domain
		headlessRule.DomainSuffix = defaultRule.DomainSuffix
		headlessRule.DomainKeyword = defaultRule.DomainKeyword
		headlessRule.DomainRegex = defaultRule.DomainRegex
		var plainRuleSet option.PlainRuleSet
		plainRuleSet.Rules = []option.HeadlessRule{
			{
				Type:           C.RuleTypeDefault,
				DefaultOptions: headlessRule,
			},
		}
		srsPath, _ := filepath.Abs(filepath.Join(ruleSetOutputDir, ruleSetOutput, "geosite-"+code+".srs"))
		os.Stderr.WriteString("write " + srsPath + "\n")
		outputRuleSet, err := os.Create(srsPath)
		if err != nil {
			return err
		}
		err = srs.Write(outputRuleSet, plainRuleSet)
		if err != nil {
			outputRuleSet.Close()
			return err
		}
		outputRuleSet.Close()
	}
	return nil
}

func release(output string, cnOutput string, ruleSetOutput string) error {
	err := generate(output, cnOutput, ruleSetOutput)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	err := release(
		"geosite.db", 
		"geosite-cn.db", 
		"rule-set",
	)
	if err != nil {
		log.Fatal(err)
	}
}
