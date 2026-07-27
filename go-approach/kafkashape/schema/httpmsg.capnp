@0xf461b3f155baae22;

using Go = import "/go.capnp";
$Go.package("kafkashape");
$Go.import("goapproach/kafkashape");

struct CpHeader {
  name  @0 :Data;
  value @1 :Data;
}

struct CpHttpPair {
  method       @0  :Data;
  path         @1  :Data;
  version      @2  :Data;
  statusCode   @3  :Int32;
  reason       @4  :Data;
  reqHeaders   @5  :List(CpHeader);
  respHeaders  @6  :List(CpHeader);
  reqBody      @7  :Data;
  respBody     @8  :Data;

  sourceIp      @9  :Text;
  destIp        @10 :Text;
  aktoAccountId @11 :Text;
  source        @12 :Text;
  daemonsetId   @13 :Text;
  processName   @14 :Text;
  tag           @15 :Text;
  timeUnix      @16 :Int64;
  vxlanId       @17 :Int32;
  direction     @18 :Int32;
  processId     @19 :UInt32;
  socketId      @20 :UInt32;
  isPending     @21 :Bool;
  enableGraph   @22 :Bool;
}
