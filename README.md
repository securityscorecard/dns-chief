# dns-chief

## Todo

* [x] CloudFlare Support
* [ ] Route53 Support

## Example config

*ops.yml*:

``` yaml
- name: testing.ops.example.com
  value: 10.10.0.123
  state: present
  type: A
  ttl: 60
```

## CloudFlare

### Usage

#### Importing from CloudFlare

``` bash
export API_KEY={{ cloudflare token }}
export EMAIL={{ cloudflare email }}
./chief-dns -zone=yourdomain.com -import
```

#### Syncing with CloudFlare

``` bash
export API_KEY={{ cloudflare token }}
export EMAIL={{ cloudflare email }}
./chief-dns -zone=yourdomain.com -sync
```

Example output:

``` bash
2016/09/25 23:04:26 Zone found: example.com
2016/09/25 23:04:27 227 remote records found.
2016/09/25 23:04:27 [config] loading: chief.yml
2016/09/25 23:04:27 1 records loaded.
2016/09/25 23:04:27 1 local records found.
2016/09/25 23:04:27 [patching] testing-chief.example.com  :: value: 127.0.0.2 -> 127.0.0.123
2016/09/25 23:04:27 [patched] testing-chief.example.com
2016/09/25 23:04:27 Consider running -operation=import to sync up differences.
2016/09/25 23:04:27 {Created:0 Removed:0 Updated:1}

```
