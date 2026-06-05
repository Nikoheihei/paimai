/**
 * ImageUploader — 图片上传组件
 *
 * 支持三种模式：
 *   1. 点击选择文件
 *   2. 拖拽上传
 *   3. URL 粘贴输入
 */

import { useState, useRef, useCallback } from 'react'
import { uploadImage } from '../api/client'  // 对接后端上传 API
import Toast from './Toast'

type Props = {
  value?: string       // 当前图片 URL
  onChange: (url: string) => void
  placeholder?: string
  maxSizeMB?: number    // 最大文件大小 MB，默认 5
  accept?: string       // 接受的文件类型
}

export default function ImageUploader({
  value,
  onChange,
  placeholder = '点击或拖拽上传图片',
  maxSizeMB = 5,
  accept = 'image/*',
}: Props) {
  const [dragging, setDragging] = useState(false)
  const [mode, setMode] = useState<'url' | 'file'>('file')
  const inputRef = useRef<HTMLInputElement>(null)

  const handleFile = useCallback((file: File) => {
    if (!file.type.startsWith('image/')) {
      Toast.error('请选择图片文件')
      return
    }
    if (file.size > maxSizeMB * 1024 * 1024) {
      Toast.error(`图片不能超过 ${maxSizeMB}MB`)
      return
    }
    // 对接后端 /api/upload 接口
    Toast.show('上传中...', 'info')
    uploadImage(file)
      .then(url => { onChange(url); Toast.success('上传成功') })
      .catch(err => { Toast.error(err.message || '上传失败') })
  }, [maxSizeMB, onChange])

  // 拖拽处理
  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault(); setDragging(true)
  }, [])
  const handleDragLeave = useCallback(() => setDragging(false), [])
  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault(); setDragging(false)
    const file = e.dataTransfer.files[0]
    if (file) handleFile(file)
  }, [handleFile])

  // 文件选择
  const handleClick = () => {
    if (value && mode === 'file') {
      // 已有图时点击不触发选择，需点"更换"
      return
    }
    inputRef.current?.click()
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (file) handleFile(file)
  }

  return (
    <div
      className={`image-uploader ${dragging ? 'dragging' : ''}`}
      onClick={handleClick}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        style={{ display: 'none' }}
        onChange={handleInputChange}
      />

      {value ? (
        <>
          <img className="iu-preview" src={value} alt="预览" onClick={(e) => e.stopPropagation()} />
          <div className="iu-actions" onClick={(e) => e.stopPropagation()}>
            <button className="iu-change-btn" onClick={() => inputRef.current?.click()}>更换图片</button>
            <button className="iu-remove-btn" onClick={() => onChange('')}>删除</button>
          </div>
        </>
      ) : (
        <div className="iu-placeholder">
          {placeholder}
          <div style={{ fontSize: 11, color: '#444', marginTop: 4 }}>或粘贴图片 URL</div>
        </div>
      )}

      {/* URL 粘贴模式切换 */}
      {!value && mode === 'file' && (
        <button
          style={{
            marginTop: 8, background: '#252540', border: 'none', borderRadius: 6,
            color: '#888', fontSize: 11, padding: '3px 10px', cursor: 'pointer',
          }}
          onClick={(e) => { e.stopPropagation(); setMode('url') }}
        >
          使用 URL 输入
        </button>
      )}

      {mode === 'url' && !value && (
        <div onClick={(e) => e.stopPropagation()} style={{ marginTop: 8 }}>
          <input
            type="text"
            placeholder="粘贴图片 URL..."
            style={{
              width: '100%', background: '#0f0f1a', border: '1px solid #3a3a5e',
              borderRadius: 8, padding: 8, color: '#e0e0e0', fontSize: 13, outline: 'none',
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                const val = (e.target as HTMLInputElement).value.trim()
                if (val) onChange(val)
              }}
            }
            onBlur={(e) => {
              const val = e.target.value.trim()
              if (val) onChange(val)
            }}
          />
          <button
            style={{ marginTop: 4, ...{ background: '#252540', border: 'none', borderRadius: 6, color: '#888', fontSize: 11, padding: '3px 10px', cursor: 'pointer' }}}
            onClick={() => setMode('file')}
          >
            返回上传
          </button>
        </div>
      )}
    </div>
  )
}
