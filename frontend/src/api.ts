import axios from 'axios';

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api/v1',
  headers: { 'Content-Type': 'application/json' },
});
const configuredBase = import.meta.env.VITE_API_BASE_URL || '/api/v1';
export const instagramApi = axios.create({ baseURL: configuredBase.replace(/\/v1\/?$/, '') || '/api', headers: { 'Content-Type': 'application/json' } });
export const documentsApi = axios.create({ baseURL: configuredBase.replace(/\/v1\/?$/, '') || '/api' });
export type Dashboard={trackedAccounts:number;analyzedPosts:number;todayAnalyzed:number;aiMatches:number;lastScanAt?:string;status:string};export type Account={id:string;platform:string;username:string;profileName:string;active:boolean;lastScannedAt?:string};export type Post={id:string;content:string;url:string;postType:string;publishedAt:string;account:Account;analysis?:{summary:string;mainTopic:string;subTopic:string;keywords:string[];sentiment:string;isRelevant:boolean;confidence:number}};
export type InstagramStatus={configured:boolean;provider?:string;demo:boolean;total_posts:number};
export type InstagramAccount={id:string;username:string;display_name?:string;profile_url:string;profile_picture_url?:string;active:boolean;last_sync_at?:string;total_posts:number;sync_error?:string;sync_status:string};
export type InstagramMedia={platform:string;username:string;external_id:string;shortcode:string;caption:string;permalink:string;media_type:string;media_url:string;thumbnail_url:string;published_at:string;collected_at:string};
export type InstagramPage={data:InstagramMedia[];meta:{page:number;limit:number;total:number}};
