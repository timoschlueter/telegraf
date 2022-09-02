# Freestyle LibreLinkUp Input Plugin

This plugin pulls blood glucose measurements from the Freestyle Libre 2 and 3 Continuous Glucose Monitoring (CGM) 
systems developed and distributed by Abbott.
Metrics are gathered in both mg/dl and mmol/l units.

## Configuration

```toml @sample.conf
# Read glucose measurements from Freestyle Libre 2 and 3 CGM sensors through the LibreLinkUp service
[[inputs.librelinkup]]
  ## Your LibreLinkUp credentials
  email = "eric.example@example.com"
  password = "mysecurepassword"

  ## (optional) patient id if you have access to shared measurements for more than one patient
  ## check the log output for all available patient ids
  # patient_id = "639dac0c-7065-4488-a782-ef81905213f3"

  ## (optional) region from where you are using librelinkup
  ## available regions: US, EU, DE, FR, JP, AP, AU, AE
  ## default is: EU
  # region = "DE"
```

## Metrics

- librelinkup
    - `mg_dl` - Current blood glucose level in mg/dl
    - `mmol_l` - Current blood glucose level in mmol/l
    - `timestamp` - Timestamp of the measurement (Unix time)

## Tags

- All measurements have the following tags:
    - `patiend_id` - UUID for the patient
    - `sensor_sn` - Serial number of the currently active CGM sensor

## Example Output

```shell
$ ./telegraf --test --config /etc/telegraf/telegraf.conf --input-filter librelinkup
* Loaded inputs: librelinkup
> librelinkup,host=hostname,patient_id=639dac0c-7065-4488-a782-ef81905213f3,sensor_sn=123ABCD456 mg_dl=222i,mmol_l=12.321,timestamp=1661751913i 1662118219000000000
```
