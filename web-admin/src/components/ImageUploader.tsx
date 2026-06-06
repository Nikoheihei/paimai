/**
 * ImageUploader — Admin 端图片上传组件
 * 支持拖拽/点击上传，对接后端 /api/upload 接口
 */

import { useState, useRef } from 'react'
import Toast from './Toast'
import { uploadImage } from '../api/client'

type Props = {
  value?: string
  onChange: (url: string) => void
  placeholder?: string
}

export default function ImageUploader({ value, onChange, placeholder = '点击或拖拽上传图片' }: Props) {
  const [dragging, setDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const handleFile = async (file: File) => {
    if (!file.type.startsWith('image/')) { Toast.error('请选择图片文件'); return }
    if (file.size > 5 * 1024 * 1024) { Toast.error('图片不能超过 5MB'); return }

    try {
      const url = await uploadImage(file)
      onChange(url)
      Toast.success('图片已上传')
    } catch (e: any) {
      Toast.error(e.message || '上传失败')
    }
  }

  return (
    <div
      className={`admin-image-uploader ${dragging ? 'dragging' : ''}`}
      onClick={() => inputRef.current?.click()}
      onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        e.preventDefault(); setDragging(false)
        const file = e.dataTransfer.files[0]
        if (file) handleFile(file)
      }}
    >
      <input ref={inputRef} type="file" accept="image/*" style={{ display: 'none' }} onChange={e => {
        const file = e.target.files?.[0]
        if (file) handleFile(file)
      }} />

      {value ? (
        <div className="aiu-preview-wrap">
          <img src={value} alt="预览" className="aiu-preview" />
          <div className="aiu-actions">
            <button className="admin-btn small" onClick={(e) => { e.stopPropagation(); inputRef.current?.click() }}>更换</button>
            <button className="admin-btn small danger" onClick={(e) => { e.stopPropagation(); onChange('') }}>删除</button>
          </div>
        </div>
      ) : (
        <div className="aiu-placeholder">{placeholder}</div>
      )}
    </div>
  )
}
