export namespace enginespike {
	
	export class Transfer {
	    revision: string;
	    state: string;
	    bytesCompleted: string;
	    length: string;
	    payloadRead: string;
	    payloadWritten: string;
	    peers: number;
	    clientsStarted: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Transfer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revision = source["revision"];
	        this.state = source["state"];
	        this.bytesCompleted = source["bytesCompleted"];
	        this.length = source["length"];
	        this.payloadRead = source["payloadRead"];
	        this.payloadWritten = source["payloadWritten"];
	        this.peers = source["peers"];
	        this.clientsStarted = source["clientsStarted"];
	        this.error = source["error"];
	    }
	}

}

