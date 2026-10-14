import { Icon } from './Icon'
import { fileChangeStatus, type FileChangeState } from './fileChangeStatus'
import './FileChangeIcon.css'

export default function FileChangeIcon({ file, layer }: { file: FileChangeState; layer?: 'index' | 'working' }) {
  const { kind, label, icon } = fileChangeStatus(file, layer)
  return <span className={`file-change-icon change-${kind}`} role="img" aria-label={label} title={label}><Icon name={icon} size={14}/></span>
}
