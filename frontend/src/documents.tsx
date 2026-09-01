import {useRef, useState} from 'react';
import type {FormEvent} from 'react';
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query';
import {AlertCircle, CheckCircle2, ExternalLink, FileText, LoaderCircle, Search, Trash2, Upload} from 'lucide-react';
import {documentsApi} from './api';
import {Error, Heading, Loading} from './App';
import './documents.css';

type Document = {
  id: string;
  filename: string;
  stored_filename: string;
  page_count: number;
  created_at: string;
};

type SearchResult = {
  document_id: string;
  filename: string;
  page: number;
  snippet: string;
  matched_keywords: string[];
};

type UploadStage = 'ready' | 'preparing' | 'uploading' | 'processing' | 'success' | 'error';

type UploadItem = {
  id: string;
  file: File;
  progress: number;
  stage: UploadStage;
  errorMessage?: string;
};

const MAX_FILES = 50;
const MAX_FILE_SIZE = 25 * 1024 * 1024;

export function Documents() {
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const [uploads, setUploads] = useState<UploadItem[]>([]);
  const [searchInput, setSearchInput] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const [match, setMatch] = useState<'any' | 'all'>('any');
  const [selectionError, setSelectionError] = useState('');

  const documents = useQuery({
    queryKey: ['documents'],
    queryFn: () => documentsApi.get<Document[]>('/documents').then(response => response.data),
  });
  const search = useQuery({
    queryKey: ['document-search', searchQuery, match],
    enabled: Boolean(searchQuery),
    queryFn: () => documentsApi.get<SearchResult[]>('/documents/search', {params: {q: searchQuery, match}}).then(response => response.data),
  });
  const updateUpload = (id: string, update: Partial<UploadItem>) => {
    setUploads(current => current.map(item => item.id === id ? {...item, ...update} : item));
  };
  const upload = useMutation({
    mutationFn: async (items: UploadItem[]) => {
      for (const item of items) {
        updateUpload(item.id, {stage: 'preparing', progress: 0, errorMessage: undefined});
        await new Promise<void>(resolve => requestAnimationFrame(() => resolve()));
        const form = new FormData();
        form.append('files', item.file);
        try {
          updateUpload(item.id, {stage: 'uploading'});
          await documentsApi.post<Document[]>('/documents/upload', form, {
            timeout: 0,
            onUploadProgress: event => {
              const ratio = event.total && event.total > 0 ? event.loaded / event.total : event.progress;
              if (ratio === undefined) {
                updateUpload(item.id, {stage: 'uploading'});
                return;
              }
              const progress = Math.min(100, Math.max(0, Math.round(ratio * 100)));
              updateUpload(item.id, {progress, stage: progress === 100 ? 'processing' : 'uploading'});
            },
          });
          updateUpload(item.id, {stage: 'success', progress: 100});
          queryClient.invalidateQueries({queryKey: ['documents']});
          queryClient.invalidateQueries({queryKey: ['document-search']});
        } catch (error) {
          updateUpload(item.id, {stage: 'error', errorMessage: uploadErrorMessage(error)});
        }
      }
    },
    onSettled: () => {
      if (fileInput.current) fileInput.current.value = '';
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => documentsApi.delete(`/documents/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({queryKey: ['documents']});
      queryClient.invalidateQueries({queryKey: ['document-search']});
    },
  });

  const selectFiles = (selected: FileList | null) => {
    const selectedFiles = Array.from(selected || []);
    const waitingCount = uploads.filter(item => item.stage === 'ready').length;
    if (waitingCount + selectedFiles.length > MAX_FILES) {
      setSelectionError('Bir seferde en fazla 50 PDF seçebilirsiniz.');
      return;
    }
    upload.reset();
    setSelectionError('');
    setUploads(current => [...current, ...selectedFiles.map((file, index): UploadItem => {
      const invalid = !isPDF(file) || file.size === 0;
      const tooLarge = file.size > MAX_FILE_SIZE;
      return {
        id: `${Date.now()}-${index}-${file.lastModified}-${file.name}`,
        file,
        progress: 0,
        stage: invalid || tooLarge ? 'error' : 'ready',
        errorMessage: invalid ? 'Geçersiz PDF dosyası.' : tooLarge ? 'Dosya 25 MB sınırını aşıyor.' : undefined,
      };
    })]);
    if (fileInput.current) fileInput.current.value = '';
  };

  const pendingUploads = uploads.filter(item => item.stage === 'ready');

  const submitSearch = (event: FormEvent) => {
    event.preventDefault();
    setSearchQuery(searchInput.trim());
  };

  return <section className="documents-page">
    <Heading title="Belgeler" text="PDF arşivi ve Türkçe karakter destekli anahtar kelime araması."/>

    <form className="card documents-upload" onSubmit={event => {event.preventDefault(); if (pendingUploads.length) upload.mutate(pendingUploads)}}>
      <label className={`file-picker${upload.isPending ? ' disabled' : ''}`}>
        <Upload size={22}/>
        <span><b>PDF dosyalarını seçin</b><small>En fazla 50 dosya · dosya başına en fazla 25 MB</small></span>
        <input ref={fileInput} type="file" accept=".pdf,application/pdf" multiple disabled={upload.isPending} onChange={event => selectFiles(event.currentTarget.files)}/>
      </label>
      <button className="primary upload-button" type="submit" disabled={!pendingUploads.length || upload.isPending}>
        <Upload size={16}/> {upload.isPending ? 'Yükleme sürüyor…' : pendingUploads.length ? `${pendingUploads.length} PDF yükle` : 'PDF yükle'}
      </button>
    </form>
    {selectionError && <Error text={selectionError}/>} 
    {uploads.length > 0 && <div className="upload-items" aria-live="polite">
      {uploads.map(item => <article className={`upload-item ${item.stage}`} key={item.id}>
        <div className="upload-item-head">
          <FileText size={18}/>
          <div><b>{item.file.name}</b><small>{formatFileSize(item.file.size)}</small></div>
          <UploadStageIcon stage={item.stage}/>
        </div>
        <div className="upload-stage"><span>{uploadStageText(item)}</span>{item.stage === 'uploading' && <b>%{item.progress}</b>}</div>
        <div className="upload-progress" role="progressbar" aria-label={`${item.file.name} yükleme ilerlemesi`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={item.progress}>
          <i style={{width: `${item.progress}%`}}/>
        </div>
        {item.stage === 'processing' && <small className="upload-note">PDF işleniyor, dosyanın boyutuna göre birkaç dakika sürebilir...</small>}
        {item.errorMessage && <small className="upload-error">{item.errorMessage}</small>}
      </article>)}
    </div>}

    <div className="documents-section-title"><h2>Yüklenen PDF'ler</h2><span>{documents.data?.length || 0} belge</span></div>
    {documents.isLoading ? <Loading/> : documents.error ? <Error text={backendMessage(documents.error, 'Belgeler alınamadı.')}/> :
      <div className="card table-wrap documents-table"><table><thead><tr><th>Dosya adı</th><th>Sayfa</th><th>Yüklenme tarihi</th><th/></tr></thead><tbody>
        {documents.data!.map(document => <tr key={document.id}><td><a href={documentFileURL(document.id)} target="_blank" rel="noreferrer"><FileText size={17}/><b>{document.filename}</b></a></td><td>{document.page_count}</td><td>{new Date(document.created_at).toLocaleString('tr-TR')}</td><td><button className="danger" type="button" aria-label={`${document.filename} belgesini sil`} disabled={remove.isPending} onClick={() => {if (window.confirm(`${document.filename} silinsin mi?`)) remove.mutate(document.id)}}><Trash2 size={16}/></button></td></tr>)}
      </tbody></table>{documents.data!.length === 0 && <div className="empty">Henüz PDF yüklenmedi.</div>}</div>}
    {remove.error && <Error text={backendMessage(remove.error, 'Belge silinemedi.')}/>} 

    <div className="documents-section-title"><h2>Anahtar kelime arama</h2></div>
    <form className="documents-search" onSubmit={submitSearch}>
      <div className="search"><Search size={19}/><input value={searchInput} onChange={event => setSearchInput(event.target.value)} placeholder="Örn. İZMİR Belediyesi"/></div>
      <select aria-label="Eşleşme yöntemi" value={match} onChange={event => setMatch(event.target.value as 'any' | 'all')}>
        <option value="any">Herhangi biri</option>
        <option value="all">Tümü</option>
      </select>
      <button className="primary" disabled={!searchInput.trim()}>Ara</button>
    </form>

    {search.isFetching && <Loading/>}
    {search.error && <Error text={backendMessage(search.error, 'Belge araması yapılamadı.')}/>} 
    {!search.isFetching && search.data && <div className="document-results">
      {search.data.map(result => <a className="card document-result" key={`${result.document_id}-${result.page}`} href={`${documentFileURL(result.document_id)}#page=${result.page}`} target="_blank" rel="noreferrer">
        <div><FileText size={18}/><b>{result.filename}</b><span>Sayfa {result.page}</span><ExternalLink size={15}/></div>
        <p>{highlight(result.snippet, result.matched_keywords)}</p>
      </a>)}
      {search.data.length === 0 && <div className="card empty">Eşleşen sayfa bulunamadı.</div>}
    </div>}
  </section>;
}

function documentFileURL(id: string) {
  const base = String(documentsApi.defaults.baseURL || '/api').replace(/\/$/, '');
  return `${base}/documents/${encodeURIComponent(id)}/file`;
}

function isPDF(file: File) {
  const mimeType = file.type.trim().toLocaleLowerCase('en-US');
  return mimeType === 'application/pdf' || file.name.trim().toLocaleLowerCase('tr-TR').endsWith('.pdf');
}

function uploadStageText(item: UploadItem) {
  switch (item.stage) {
    case 'ready': return 'Yüklemeye hazır';
    case 'preparing': return 'Dosya hazırlanıyor';
    case 'uploading': return 'Yükleniyor';
    case 'processing': return 'PDF işleniyor...';
    case 'success': return 'Tamamlandı';
    case 'error': return 'Yükleme başarısız';
  }
}

function UploadStageIcon({stage}: {stage: UploadStage}) {
  if (stage === 'success') return <CheckCircle2 className="upload-success-icon" size={20} aria-label="Başarılı"/>;
  if (stage === 'error') return <AlertCircle className="upload-error-icon" size={20} aria-label="Başarısız"/>;
  if (stage === 'preparing' || stage === 'uploading' || stage === 'processing') return <LoaderCircle className="upload-spinner" size={20} aria-label="İşleniyor"/>;
  return null;
}

function formatFileSize(size: number) {
  if (size < 1024 * 1024) return `${Math.max(1, Math.round(size / 1024))} KB`;
  return `${(size / (1024 * 1024)).toLocaleString('tr-TR', {maximumFractionDigits: 1})} MB`;
}

function uploadErrorMessage(error: unknown) {
  const typedError = error as {code?: string; response?: {status?: number}};
  const status = typedError.response?.status;
  const backend = backendMessage(error, '');
  if (!typedError.response || typedError.code === 'ERR_NETWORK' || typedError.code === 'ECONNABORTED') {
    return 'Bağlantı kesildi. İnternet bağlantınızı kontrol edip yeniden deneyin.';
  }
  if (status === 400) return backend || 'Geçersiz veya işlenemeyen PDF dosyası.';
  if (status === 413) return backend || 'PDF dosyası izin verilen boyut sınırını aşıyor.';
  if (status === 502 || status === 503) return backend ? `PDF işlenemedi. ${backend}` : 'PDF işlenemedi. Lütfen yeniden deneyin.';
  if (status && status >= 500) return backend ? `Sunucu hatası. ${backend}` : 'Sunucu hatası oluştu. Lütfen yeniden deneyin.';
  return backend || 'Yükleme başarısız oldu. Lütfen yeniden deneyin.';
}

function backendMessage(error: unknown, fallback: string) {
  const typedError = error as {message?: string; response?: {data?: unknown}};
  const data = typedError.response?.data;
  if (typeof data === 'string' && data.trim()) return data;
  if (data && typeof data === 'object') {
    const body = data as {message?: string; detail?: string | {message?: string}; error?: string | {message?: string}};
    if (typeof body.error === 'string') return body.error;
    if (body.error?.message) return body.error.message;
    if (typeof body.detail === 'string') return body.detail;
    if (body.detail?.message) return body.detail.message;
    if (body.message) return body.message;
  }
  return typedError.message || fallback;
}

function highlight(text: string, keywords: string[]) {
  if (!keywords.length) return text;
  const variants: Record<string, string> = {c: '[cç]', g: '[gğ]', i: '[iİIı]', o: '[oö]', s: '[sş]', u: '[uü]'};
  const patterns = keywords.map(keyword => normalize(keyword)).filter(Boolean).map(keyword => Array.from(keyword).map(character => variants[character] || escapeRegex(character)).join(''));
  if (!patterns.length) return text;
  const parts = text.split(new RegExp(`(${patterns.join('|')})`, 'giu'));
  return parts.map((part, index) => index % 2 === 1 ? <mark key={index}>{part}</mark> : part);
}

function normalize(value: string) {
  return value.toLocaleLowerCase('tr-TR').replace(/[İIı]/g, 'i').replace(/[şŞ]/g, 's').replace(/[ğĞ]/g, 'g').replace(/[üÜ]/g, 'u').replace(/[öÖ]/g, 'o').replace(/[çÇ]/g, 'c');
}

function escapeRegex(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
