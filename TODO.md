

## TODO LIST ##

* OAUTH API for posting
* sqlite database type
* redis database type

* static JSON files for http frontend
* reprocess nntp articles admin function
* thoroughly fix nntp sync deadlocks

src/srnd/config.go 

Setup() method needs to catch errors.
ReadConfig() : Should take config file name as argument. Should return error type if erorr occurs.

func ReadConfig(filename string) (*SRNdConfig, error){}


-----------

Validate() should also return errors if it fails: Needs to change to:

func (self *SRNdConfig) Validate() error {
	
}


------------------

src/srnd/database.go

NewDatabase(par1,par2,..) should return Database and error

Should change to:

func NewDatabase(db_type, schema, host, port, user, password string) (Database,error)  {
