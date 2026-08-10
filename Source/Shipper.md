# Shipper  

> **Superseded — kept as the original concept note, not as a specification.**
>
> This was the first sketch of the product and it has since been overtaken in three
> material ways. It describes a **Next.js client with a Next.js BFF**; there is no BFF
> tier and the client is **Flutter** (`Docs/06` §2.1, §7). It lists roles as
> **Driver / Customer / Admin**; the roles are **customer, provider, assigned driver and
> administrator**, with the assigned driver holding no account at all (`Docs/01` §3). And
> it treats Redis as a BFF cache; Redis backs the device registry, idempotency keys and
> rate limits (`Docs/06` §2.1).
>
> **`Docs/01`–`10` are the specification.** Read this only for the original intent.

This application is a bidding platform for good transportation services located in Australia.  
2 Applications and 3 user roles  
1. Client app  
	- Driver  
	- Customer  
1. AP  
	- Admin  
##   
## Main Goal of the application  
Customer POV - To provide and competitive marketplace where customers can find cheap / reliable delivery partners for their delivery jobs.  
Driver POV - To make market / industry less resistant for small business owners (Transport service providers) and find new customers in a competitive environment.  
##   
## Client App  
### Customers can place their job:  
- They can decide the minimum bid amount  
- Desired delivery date  
- Vehicle type  
- Pickup and drop-off location  
- Track their jobs  
- Negotiate with drivers  
  
### Drivers can bid for jobs:  
- They can negotiate with customers  
- Can accept or reject the job offer  
- They can enter their available vehicles  
- They can show their specialities and offerings to customers  
- Should be able to generate a job scoped web app  portal link to track the job.  
- This job scoped web app portal is shared to the person who drive the vehicle.  
  
## AP  
### Admins can manage users:  
- They have all permissions to monitor the system  
- They can handle users  
- They are acting as customer service for any inquiry  
  
## Job:  
This is the smallest unit of the system  
Created by customers  
It is a good delivery task  
  
## Good:  
Good is the delivery item  
This should not be an entity of alive (Humans , Animals)  
This should not be any illegal item  
  
## Driver:  
This is the vehicle owner.  
Can register multiple vehicles  
  
## Customer:  
The one, who create delivery jobs  
Have all rights to offer the job and track the job  
  
  
## Preferred Tech stack;  
- Client app - nextJS  
- BFF - nextJS  
- BE - Go  
- Cache (BFF) - redis  
- DB - Postgres  
- Queues - Kafka  
- Cloud  -AWS  
- Logging - Datadog
  
  
  
