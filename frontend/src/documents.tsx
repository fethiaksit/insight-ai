import {useState} from 'react';
import type {FormEvent} from 'react';
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query';
import {ExternalLink, FileText, Search, Trash2, Upload} from 'lucide-react';
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

const MAX_FILES = 50;
const MAX_FILE_SIZE = 25 * 1024 * 1024;

export function Documents() {
  const queryClient = useQueryClient();
  const [files, setFiles] = useState<File[]>([]);
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
  const upload = useMutation({
    mutationFn: (selectedFiles: File[]) => {
      const form = new FormData();
      selectedFiles.forEach(file => form.append('files', file));
      return documentsApi.post<Document[]>('/documents/upload', form).then(response => response.data);
    },
    onSuccess: () => {
      setFiles([]);
      setSelectionError('');
      queryClient.invalidateQueries({queryKey: ['documents']});
      queryClient.invalidateQueries({queryKey: ['document-search']});
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
    const next = [...files, ...selectedFiles];
    if (next.length > MAX_FILES) {
      setSelectionError('Bir seferde en fazla 50 PDF seçebilirsiniz.');
      return;
    }
    const invalid = selectedFiles.find(file => !file.name.toLocaleLowerCase('tr-TR').endsWith('.pdf'));
    if (invalid) {
      setSelectionError(`${invalid.name} geçerli bir PDF değil.`);
      return;
    }
    const tooLarge = selectedFiles.find(file => file.size > MAX_FILE_SIZE);
    if (tooLarge) {
      setSelectionError(`${tooLarge.name} 25 MB sınırını aşıyor.`);
      return;
    }
    upload.reset();
    setSelectionError('');
    setFiles(next);
  };

  const submitSearch = (event: FormEvent) => {
    event.preventDefault();
    setSearchQuery(searchInput.trim());
  };

  return <section className="documents-page">
    <Heading title="Belgeler" text="PDF arşivi ve Türkçe karakter destekli anahtar kelime araması."/>

    <form className="card documents-upload" onSubmit={event => {event.preventDefault(); if (files.length) upload.mutate(files)}}>
      <label className="file-picker">
        <Upload size={22}/>
        <span><b>PDF dosyalarını seçin</b><small>En fazla 50 dosya · dosya başına en fazla 25 MB</small></span>
        <input type="file" accept="application/pdf,.pdf" multiple disabled={upload.isPending} onChange={event => {selectFiles(event.currentTarget.files); event.currentTarget.value = ''}}/>
      </label>
      <button className="primary" disabled={!files.length || upload.isPending}>
        <Upload size={16}/> {upload.isPending ? 'İşleniyor…' : files.length ? `${files.length} PDF yükle` : 'PDF yükle'}
      </button>
    </form>
    {selectionError && <Error text={selectionError}/>} 
    {upload.error && <Error text={backendMessage(upload.error, 'PDF dosyaları yüklenemedi.')}/>} 

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
