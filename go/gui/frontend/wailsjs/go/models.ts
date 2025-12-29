export namespace gui {
	
	export class Settings {
	    config_path: string;
	    chat_id: string;
	    topic_id?: number;
	    watch_dir: string;
	    queue_file: string;
	    recursive: boolean;
	    with_image: boolean;
	    with_video: boolean;
	    with_audio: boolean;
	    with_all: boolean;
	    include?: string[];
	    exclude?: string[];
	    zip_passwords?: string[];
	    zip_pass_file: string;
	    scan_interval_sec: number;
	    send_interval_sec: number;
	    settle_seconds: number;
	    group_size: number;
	    batch_delay_sec: number;
	    pause_every: number;
	    pause_seconds_sec: number;
	    notify_enabled: boolean;
	    notify_interval_sec: number;
	    max_dimension: number;
	    max_bytes: number;
	    png_start_level: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config_path = source["config_path"];
	        this.chat_id = source["chat_id"];
	        this.topic_id = source["topic_id"];
	        this.watch_dir = source["watch_dir"];
	        this.queue_file = source["queue_file"];
	        this.recursive = source["recursive"];
	        this.with_image = source["with_image"];
	        this.with_video = source["with_video"];
	        this.with_audio = source["with_audio"];
	        this.with_all = source["with_all"];
	        this.include = source["include"];
	        this.exclude = source["exclude"];
	        this.zip_passwords = source["zip_passwords"];
	        this.zip_pass_file = source["zip_pass_file"];
	        this.scan_interval_sec = source["scan_interval_sec"];
	        this.send_interval_sec = source["send_interval_sec"];
	        this.settle_seconds = source["settle_seconds"];
	        this.group_size = source["group_size"];
	        this.batch_delay_sec = source["batch_delay_sec"];
	        this.pause_every = source["pause_every"];
	        this.pause_seconds_sec = source["pause_seconds_sec"];
	        this.notify_enabled = source["notify_enabled"];
	        this.notify_interval_sec = source["notify_interval_sec"];
	        this.max_dimension = source["max_dimension"];
	        this.max_bytes = source["max_bytes"];
	        this.png_start_level = source["png_start_level"];
	    }
	}
	export class TelegramConfig {
	    api_urls: string[];
	    tokens: string[];
	
	    static createFrom(source: any = {}) {
	        return new TelegramConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.api_urls = source["api_urls"];
	        this.tokens = source["tokens"];
	    }
	}

}

export namespace main {
	
	export class RunStatus {
	    running: boolean;
	    paused: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RunStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.paused = source["paused"];
	        this.error = source["error"];
	    }
	}
	export class SendFilesRequest {
	    send_type: string;
	    file_path: string;
	    dir_path: string;
	    zip_file: string;
	    start_index: number;
	    end_index: number;
	    batch_delay_sec: number;
	    enable_zip: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SendFilesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.send_type = source["send_type"];
	        this.file_path = source["file_path"];
	        this.dir_path = source["dir_path"];
	        this.zip_file = source["zip_file"];
	        this.start_index = source["start_index"];
	        this.end_index = source["end_index"];
	        this.batch_delay_sec = source["batch_delay_sec"];
	        this.enable_zip = source["enable_zip"];
	    }
	}
	export class SendImagesRequest {
	    image_dir: string;
	    zip_file: string;
	    group_size: number;
	    start_index: number;
	    end_index: number;
	    batch_delay_sec: number;
	    enable_zip: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SendImagesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.image_dir = source["image_dir"];
	        this.zip_file = source["zip_file"];
	        this.group_size = source["group_size"];
	        this.start_index = source["start_index"];
	        this.end_index = source["end_index"];
	        this.batch_delay_sec = source["batch_delay_sec"];
	        this.enable_zip = source["enable_zip"];
	    }
	}
	export class SettingsBundle {
	    settings: gui.Settings;
	    telegram: gui.TelegramConfig;
	    settings_path: string;
	
	    static createFrom(source: any = {}) {
	        return new SettingsBundle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], gui.Settings);
	        this.telegram = this.convertValues(source["telegram"], gui.TelegramConfig);
	        this.settings_path = source["settings_path"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

