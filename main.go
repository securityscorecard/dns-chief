package main

import (
	//"fmt"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/crackcomm/cloudflare"
	"golang.org/x/net/context"
	"gopkg.in/yaml.v2"
)

type Stats struct {
	Created int
	Removed int
	Updated int
}

type ChiefRecord struct {
	Name   string
	Value  string
	Type   string
	TTL    int
	record *cloudflare.Record
}

func main() {
	client := cloudflare.New(&cloudflare.Options{
		Email: os.Getenv("EMAIL"),
		Key:   os.Getenv("API_KEY"),
	})
	stats := Stats{}

	var zoneName string
	var zone *cloudflare.Zone
	var operation string

	flag.StringVar(&zoneName, "zone", "", "specify zone to manage")
	flag.StringVar(&operation, "operation", "", "operation to perform: import (dump CF records to yaml)")
	flag.Parse()

	if len(zoneName) == 0 {
		log.Fatal("-zone is required")
	}

	if len(operation) == 0 {
		log.Fatal("-operation is required")
	}

	ctx := context.Background()
	ctx, _ = context.WithTimeout(ctx, time.Second*30)

	zones, err := client.Zones.List(ctx)
	if err != nil {
		log.Fatal(err)
	} else if len(zones) == 0 {
		log.Fatal("No zones were found")
	}

	for _, z := range zones {
		if z.Name == zoneName {
			zone = z
		}
	}

	if zone == nil {
		log.Fatal("Zone not found: ", zoneName)
	}

	log.Println("Zone found:", zone.Name)

	// TODO: extract into method for refreshing records
	records, err := client.Records.List(ctx, zone.ID)
	if err != nil {
		log.Fatal(err)
	}
	chiefRecords := []ChiefRecord{}
	for _, record := range records {
		tmp := ChiefRecord{Name: record.Name, Value: record.Content,
			Type: record.Type, TTL: record.TTL, record: record}
		chiefRecords = append(chiefRecords, tmp)
	}

	log.Println(len(chiefRecords), "remote records found.")
	// end TODO

	if operation == "import" {
		out, err := yaml.Marshal(&chiefRecords)
		if err != nil {
			log.Fatal(err)
		}
		err = ioutil.WriteFile("chief.yml", out, 0644)
		log.Println("records imported to chief.yml")
	}

	if operation == "sync" {
		// sync up differences - loop over local, verify in remote,
		// log msg of missing records in local
		// if not in remote but in local, add/delete depending on state cmd

		files, err := ioutil.ReadDir(".")
		if err != nil {
			log.Fatal("Error reading configs: ", err)
		}

		localRecords := []ChiefRecord{}

		for _, file := range files {
			fileMeta := strings.Split(file.Name(), ".")
			if len(fileMeta) >= 2 && fileMeta[len(fileMeta)-1] == "yml" {
				log.Println("[config] loading:", file.Name())
				chiefYML, err := ioutil.ReadFile(file.Name())
				if err != nil {
					log.Fatal(err)
				}

				tmp := []ChiefRecord{}
				err = yaml.Unmarshal(chiefYML, &tmp)
				localRecords = append(localRecords, tmp...)
				log.Println(len(tmp), "records loaded.")
			}
		}

		log.Println(len(localRecords), "local records found.")

		for _, r := range localRecords {
			if !exists(chiefRecords, r, zone) {
				log.Println("[creating]", r.Name, "(", r.Value, ") doesn't exist in remote.")
				createRecord(r, client, ctx, zone)
				stats.Created++
			}

			updated := checkPatch(chiefRecords, r, zone, client, ctx)
			if updated {
				stats.Updated++
			}
		}

		if len(localRecords) != len(chiefRecords) {
			log.Println("Consider running -operation=import to sync up differences.")
		}

		log.Printf("%+v\n", stats)
	}
}

func createRecord(record ChiefRecord, client *cloudflare.Client,
	ctx context.Context, zone *cloudflare.Zone) {

	cfRecord := &cloudflare.Record{Type: record.Type, Name: record.Name,
		Content: record.Value, TTL: record.TTL, ZoneID: zone.ID, ZoneName: zone.Name}
	err := client.Records.Create(ctx, cfRecord)
	if err != nil {
		log.Fatal("Error creating record: ", err)
	}

}

func checkPatch(remoteRecords []ChiefRecord, localRecord ChiefRecord,
	zone *cloudflare.Zone, client *cloudflare.Client, ctx context.Context) bool {

	fqdn := fmt.Sprintf("%s.%s", localRecord.Name, zone.Name)
	var remoteRecord *ChiefRecord

	for _, r := range remoteRecords {
		// just check for the name
		// dont match on value/ttl since that could be getting updated
		if r.Name == localRecord.Name || fqdn == r.Name {
			remoteRecord = &r
			break
		}
	}

	if remoteRecord == nil {
		log.Fatal("Cannot find remote record for:", localRecord.Name)
	}

	patch := false

	if remoteRecord.Value != localRecord.Value {
		patch = true
		log.Println("[patching]", localRecord.Name, " :: value:", remoteRecord.Value, "->", localRecord.Value)
	}
	if remoteRecord.TTL != localRecord.TTL {
		patch = true
		log.Println("[patching]", localRecord.Name, " :: ttl:", remoteRecord.TTL, "->", localRecord.TTL)
	}
	if remoteRecord.Type != localRecord.Type {
		patch = true
		log.Println("[patching]", localRecord.Name, " :: type:", remoteRecord.Type, "->", localRecord.Type)
	}

	if patch {
		cfRecord := &cloudflare.Record{Type: localRecord.Type, Name: localRecord.Name, ID: remoteRecord.record.ID,
			Content: localRecord.Value, TTL: localRecord.TTL, ZoneID: zone.ID, ZoneName: zone.Name}
		err := client.Records.Patch(ctx, cfRecord)
		if err != nil {
			log.Fatal("Error patching record:", err)
		}
		log.Println("[patched]", localRecord.Name)
		return true
	}
	return false
}

func exists(records []ChiefRecord, record ChiefRecord, zone *cloudflare.Zone) bool {
	fqdn := fmt.Sprintf("%s.%s", record.Name, zone.Name)
	for _, r := range records {
		// just check for the name
		// dont match on value/ttl since that could be getting updated
		if r.Name == record.Name || fqdn == r.Name {
			return true
		}
	}
	return false
}
